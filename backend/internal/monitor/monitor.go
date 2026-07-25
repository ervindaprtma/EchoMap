// Package monitor holds the state-change logic: debounce + flap detection (Doc 3 §2).
// It is deliberately decoupled from Postgres/Redis via small interfaces so the
// state machine can be unit-tested with fakes.
package monitor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"echomap/internal/bus"
)

const (
	DebounceCount = 3                // consecutive identical probes to confirm a flip
	FlapWindow    = 10 * time.Minute // window for counting confirmed transitions
	FlapThreshold = 5                // > this many transitions in the window => flapping
)

type Store interface {
	ApplyStatusChange(ctx context.Context, id int64, from, to string, alive bool) error
	RecordTransition(ctx context.Context, id int64, to string) error
	CountTransitions(ctx context.Context, id int64, window time.Duration) (int, error)
	SetFlapping(ctx context.Context, id int64, flapping bool) error
	CascadeDown(ctx context.Context, parentID int64) ([]Child, error)
	CascadeUp(ctx context.Context, parentID int64) ([]Child, error)
}

type Publisher interface {
	PublishStatus(ctx context.Context, ev bus.StatusEvent) error
}

// Child is a descendant touched by a dependency cascade (Doc 3 §5).
type Child struct {
	ID   int64
	IP   string
	Name string
	From string // status the child had before the cascade touched it
}

// Alerter delivers state-change notifications. Aggregation contract (Doc 3 §5):
// one alert per confirmed parent transition carrying its affected children;
// the children themselves never alert.
type Alerter interface {
	StatusAlert(ctx context.Context, d Device, from, to string, affectedChildren []Child)
}

type Metrics interface {
	WritePing(id int64, ip, site string, alive bool, rttMs float64)
}

// EventLogger records confirmed transitions to the event log (Doc 3 §10). Optional
// (nil = no logging); the store satisfies it. Fire-and-forget: a logging failure
// must never fail a probe.
type EventLogger interface {
	Info(ctx context.Context, category, message string, deviceID *int64)
}

type Device struct {
	ID     int64
	IP     string
	Name   string
	Site   string
	Status string // current DB status; seeds runtime state on first sight
}

type devState struct {
	status     string
	upStreak   int
	downStreak int
	isFlapping bool
}

type Monitor struct {
	store   Store
	pub     Publisher
	metrics Metrics

	// Alerter is optional (nil = no alerting); set after New. Kept a field, not a
	// constructor param, so the api role and tests don't have to care.
	Alerter Alerter

	// Events is optional (nil = no event logging); set after New, same rationale.
	Events EventLogger

	mu    sync.Mutex
	state map[int64]*devState
}

func New(store Store, pub Publisher, metrics Metrics) *Monitor {
	return &Monitor{store: store, pub: pub, metrics: metrics, state: map[int64]*devState{}}
}

// Evaluate processes one probe result (Doc 3 §2). The caller guarantees a device
// is evaluated by at most one goroutine at a time (one probe per device per tick),
// so devState needs no per-field locking — only the map does.
func (m *Monitor) Evaluate(ctx context.Context, d Device, alive bool, rttMs float64) {
	if m.metrics != nil {
		m.metrics.WritePing(d.ID, d.IP, d.Site, alive, rttMs)
	}

	st := m.getState(d.ID, d.Status)

	// 0) Reconcile with the DB status. The api process (a separate process from
	// this worker) may reset a device's status — e.g. clearing a cascade victim
	// to UNKNOWN on re-parent/delete, or future orphan restores. In-memory and
	// DB status otherwise only diverge through those api-side writes, so on
	// mismatch adopt the DB value and restart the debounce.
	if st.status != d.Status {
		st.status = d.Status
		st.upStreak, st.downStreak = 0, 0
	}

	// 1) Debounce streaks.
	if alive {
		st.upStreak++
		st.downStreak = 0
	} else {
		st.downStreak++
		st.upStreak = 0
	}

	newStatus := st.status
	if alive && st.upStreak >= DebounceCount && st.status != "UP" {
		newStatus = "UP"
	}
	if !alive && st.downStreak >= DebounceCount && st.status != "DOWN" {
		newStatus = "DOWN"
	}
	if newStatus == st.status {
		return // no confirmed change: metrics only, no fan-out
	}

	// 2) Confirmed transition — persist before publishing.
	from := st.status
	st.status = newStatus
	if err := m.store.ApplyStatusChange(ctx, d.ID, from, newStatus, alive); err != nil {
		st.status = from // roll back so the next probe retries the transition
		return
	}
	_ = m.store.RecordTransition(ctx, d.ID, newStatus)

	// 3) Flap flag (alert *suppression* itself lands in Slice 7).
	if count, err := m.store.CountTransitions(ctx, d.ID, FlapWindow); err == nil {
		switch {
		case count > FlapThreshold && !st.isFlapping:
			st.isFlapping = true
			_ = m.store.SetFlapping(ctx, d.ID, true)
		case count <= FlapThreshold && st.isFlapping:
			st.isFlapping = false
			_ = m.store.SetFlapping(ctx, d.ID, false)
		}
	}

	// 4) Fan out the confirmed transition (node recolor).
	reason := ""
	if newStatus == "DOWN" {
		reason = "PROBE"
	}
	m.publish(ctx, d, from, newStatus, st.isFlapping, alive, rttMs, reason)

	// Event log (INFO) for the confirmed transition (Doc 3 §10). Cascade children
	// are logged by the sync/cascade path, not here — this is the device's own probe.
	if m.Events != nil {
		id := d.ID
		m.Events.Info(ctx, "device", fmt.Sprintf("%s (%s) %s → %s", d.Name, d.IP, from, newStatus), &id)
	}

	// 5) Parent/child dependency cascade (Doc 3 §5).
	affected := m.cascade(ctx, d, newStatus)

	// 6) One aggregated alert for this device; cascaded children stay silent.
	// While flapping, per-transition alerts are suppressed too.
	// ponytail: the single "flapping" alert itself lands with the full notifier in Slice 7.
	if m.Alerter != nil && !st.isFlapping {
		m.Alerter.StatusAlert(ctx, d, from, newStatus, affected)
	}
}

// cascade applies Doc 3 §5. A confirmed DOWN drags every descendant down with
// down_reason=PARENT (ListForMonitoring stops probing them); a confirmed UP
// releases cascade victims to UNKNOWN so their next probe resolves them.
// Child transitions are published for the map but NOT recorded in
// status_transitions — a flapping parent must not brand its children as flapping.
func (m *Monitor) cascade(ctx context.Context, parent Device, to string) []Child {
	var (
		kids                 []Child
		err                  error
		childTo, childReason string
	)
	switch to {
	case "DOWN":
		kids, err = m.store.CascadeDown(ctx, parent.ID)
		childTo, childReason = "DOWN", "PARENT"
	case "UP":
		kids, err = m.store.CascadeUp(ctx, parent.ID)
		childTo = "UNKNOWN"
	default:
		return nil
	}
	if err != nil || len(kids) == 0 {
		return nil
	}

	loss := 100.0
	for _, c := range kids {
		// Sync the in-memory debounce state so the child's next probe evaluates
		// against its cascaded status, not a stale one. A child probe racing this
		// write can at worst publish one duplicate transition; the next sweep heals it.
		m.setState(c.ID, childTo)

		ev := bus.StatusEvent{
			DeviceID: c.ID, IPAddress: c.IP, Name: c.Name,
			FromStatus: c.From, ToStatus: childTo, DownReason: childReason,
			ChangedAt: time.Now(),
		}
		if childTo == "DOWN" {
			ev.PacketLoss = &loss
		}
		if m.pub != nil {
			_ = m.pub.PublishStatus(ctx, ev)
		}
	}
	return kids
}

// setState force-resets a device's runtime state (cascade writes, not probes).
func (m *Monitor) setState(id int64, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state[id] = &devState{status: status}
}

func (m *Monitor) getState(id int64, seed string) *devState {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.state[id]
	if st == nil {
		st = &devState{status: seed}
		m.state[id] = st
	}
	return st
}

func (m *Monitor) publish(ctx context.Context, d Device, from, to string, flapping, alive bool, rttMs float64, reason string) {
	if m.pub == nil {
		return
	}
	ev := bus.StatusEvent{
		DeviceID: d.ID, IPAddress: d.IP, Name: d.Name,
		FromStatus: from, ToStatus: to, IsFlapping: flapping,
		DownReason: reason,
		ChangedAt:  time.Now(),
	}
	loss := 100.0
	if alive {
		lat, zero := rttMs, 0.0
		ev.LatencyMs, ev.PacketLoss = &lat, &zero
	} else {
		ev.PacketLoss = &loss
	}
	_ = m.pub.PublishStatus(ctx, ev)
}
