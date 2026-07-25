package monitor

import (
	"context"
	"testing"
	"time"

	"echomap/internal/bus"
)

type fakeStore struct {
	transitions int // value CountTransitions returns; RecordTransition increments it
	flapSet     *bool
	kids        []Child // returned by CascadeDown/CascadeUp
	downCalls   int
	upCalls     int
}

func (f *fakeStore) ApplyStatusChange(context.Context, int64, string, string, bool) error { return nil }
func (f *fakeStore) RecordTransition(context.Context, int64, string) error {
	f.transitions++
	return nil
}
func (f *fakeStore) CountTransitions(context.Context, int64, time.Duration) (int, error) {
	return f.transitions, nil
}
func (f *fakeStore) SetFlapping(_ context.Context, _ int64, flapping bool) error {
	f.flapSet = &flapping
	return nil
}
func (f *fakeStore) CascadeDown(context.Context, int64) ([]Child, error) {
	f.downCalls++
	return f.kids, nil
}
func (f *fakeStore) CascadeUp(context.Context, int64) ([]Child, error) {
	f.upCalls++
	return f.kids, nil
}

type fakeAlerter struct {
	calls    int
	lastTo   string
	affected []Child
}

func (a *fakeAlerter) StatusAlert(_ context.Context, _ Device, _, to string, affected []Child) {
	a.calls++
	a.lastTo = to
	a.affected = affected
}

type fakePub struct{ events []bus.StatusEvent }

func (p *fakePub) PublishStatus(_ context.Context, ev bus.StatusEvent) error {
	p.events = append(p.events, ev)
	return nil
}

func TestDebounceRequiresConsecutiveProbes(t *testing.T) {
	st, pub := &fakeStore{}, &fakePub{}
	m := New(st, pub, nil)
	d := Device{ID: 1, IP: "10.0.0.1", Status: "UP"}
	ctx := context.Background()

	// Two DOWN probes is below DebounceCount(3) -> no confirmed change.
	m.Evaluate(ctx, d, false, 0)
	m.Evaluate(ctx, d, false, 0)
	if len(pub.events) != 0 {
		t.Fatalf("expected no transition before debounce, got %d events", len(pub.events))
	}

	// Third consecutive DOWN confirms UP->DOWN.
	m.Evaluate(ctx, d, false, 0)
	if len(pub.events) != 1 {
		t.Fatalf("expected 1 transition event, got %d", len(pub.events))
	}
	if pub.events[0].FromStatus != "UP" || pub.events[0].ToStatus != "DOWN" {
		t.Fatalf("unexpected event: %+v", pub.events[0])
	}

	// A single UP probe must not flip it back (needs DebounceCount again).
	m.Evaluate(ctx, d, true, 1.0)
	if len(pub.events) != 1 {
		t.Fatalf("single UP probe should not confirm recovery, got %d events", len(pub.events))
	}
}

func TestFlappingFlagSetWhenTransitionsExceedThreshold(t *testing.T) {
	st, pub := &fakeStore{transitions: FlapThreshold}, &fakePub{} // next transition pushes count over
	m := New(st, pub, nil)
	d := Device{ID: 1, IP: "10.0.0.1", Status: "UP"}
	ctx := context.Background()

	for i := 0; i < DebounceCount; i++ {
		m.Evaluate(ctx, d, false, 0) // confirm DOWN
	}
	if st.flapSet == nil || !*st.flapSet {
		t.Fatalf("expected flapping flag set true, got %v", st.flapSet)
	}
	if last := pub.events[len(pub.events)-1]; !last.IsFlapping {
		t.Fatalf("expected published event to carry is_flapping=true, got %+v", last)
	}
}

func TestParentDownCascadesToChildren(t *testing.T) {
	kids := []Child{
		{ID: 2, IP: "10.0.0.2", Name: "child-1", From: "UP"},
		{ID: 3, IP: "10.0.0.3", Name: "child-2", From: "UNKNOWN"},
	}
	st, pub, al := &fakeStore{kids: kids}, &fakePub{}, &fakeAlerter{}
	m := New(st, pub, nil)
	m.Alerter = al
	d := Device{ID: 1, IP: "10.0.0.1", Name: "parent", Status: "UP"}
	ctx := context.Background()

	for i := 0; i < DebounceCount; i++ {
		m.Evaluate(ctx, d, false, 0) // confirm parent DOWN
	}

	// Events: parent DOWN (reason PROBE) + one per child (reason PARENT).
	if len(pub.events) != 3 {
		t.Fatalf("expected 3 events (parent + 2 children), got %d", len(pub.events))
	}
	if pub.events[0].DownReason != "PROBE" {
		t.Fatalf("parent event should carry down_reason=PROBE, got %q", pub.events[0].DownReason)
	}
	for _, ev := range pub.events[1:] {
		if ev.ToStatus != "DOWN" || ev.DownReason != "PARENT" {
			t.Fatalf("child event should be DOWN/PARENT, got %+v", ev)
		}
	}
	// One aggregated alert, carrying both children — never one per child.
	if al.calls != 1 || len(al.affected) != 2 || al.lastTo != "DOWN" {
		t.Fatalf("expected 1 aggregated DOWN alert with 2 children, got calls=%d affected=%d to=%s",
			al.calls, len(al.affected), al.lastTo)
	}
	// Child runtime state must now read DOWN so its next probe evaluates fresh.
	if got := m.getState(2, "X").status; got != "DOWN" {
		t.Fatalf("child in-memory state = %q, want DOWN", got)
	}
}

func TestApiSideStatusResetIsAdopted(t *testing.T) {
	st, pub := &fakeStore{}, &fakePub{}
	m := New(st, pub, nil)
	ctx := context.Background()

	// Worker confirms the device DOWN the normal way.
	d := Device{ID: 1, IP: "10.0.0.1", Status: "UP"}
	for i := 0; i < DebounceCount; i++ {
		m.Evaluate(ctx, d, false, 0)
	}
	if len(pub.events) != 1 || pub.events[0].ToStatus != "DOWN" {
		t.Fatalf("setup: expected confirmed DOWN, got %+v", pub.events)
	}

	// The api process resets the row to UNKNOWN (re-parent/delete flows). The
	// worker must adopt that and re-confirm from a fresh debounce window —
	// without reconciliation the stale in-memory DOWN would suppress the
	// transition and the device would read UNKNOWN forever.
	d.Status = "UNKNOWN"
	for i := 0; i < DebounceCount; i++ {
		m.Evaluate(ctx, d, true, 1.0)
	}
	last := pub.events[len(pub.events)-1]
	if last.FromStatus != "UNKNOWN" || last.ToStatus != "UP" {
		t.Fatalf("expected UNKNOWN->UP after api-side reset, got %+v", last)
	}
}

func TestParentRecoveryReleasesCascadedChildren(t *testing.T) {
	kids := []Child{{ID: 2, IP: "10.0.0.2", Name: "child-1", From: "DOWN"}}
	st, pub, al := &fakeStore{kids: kids}, &fakePub{}, &fakeAlerter{}
	m := New(st, pub, nil)
	m.Alerter = al
	d := Device{ID: 1, IP: "10.0.0.1", Name: "parent", Status: "DOWN"}
	ctx := context.Background()

	for i := 0; i < DebounceCount; i++ {
		m.Evaluate(ctx, d, true, 1.0) // confirm parent UP
	}

	if st.upCalls != 1 || st.downCalls != 0 {
		t.Fatalf("expected exactly one CascadeUp, got up=%d down=%d", st.upCalls, st.downCalls)
	}
	// Child is released to UNKNOWN (next probe resolves it), with no down_reason.
	last := pub.events[len(pub.events)-1]
	if last.DeviceID != 2 || last.ToStatus != "UNKNOWN" || last.DownReason != "" {
		t.Fatalf("child release event should be UNKNOWN with no reason, got %+v", last)
	}
	if got := m.getState(2, "X").status; got != "UNKNOWN" {
		t.Fatalf("child in-memory state = %q, want UNKNOWN", got)
	}
}
