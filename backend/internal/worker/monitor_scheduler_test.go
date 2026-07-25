package worker

import "testing"

// A confirmed transition requires DebounceCount (3) consecutive identical results,
// mirroring the ping loop — one blip must not flip the monitor.
func TestStepDebounce(t *testing.T) {
	// From UNKNOWN, three OKs confirm UP exactly on the third.
	status, up, down := "UNKNOWN", 0, 0
	for i := 1; i <= 3; i++ {
		var ch bool
		status, up, down, ch = stepDebounce(status, up, down, true)
		if i < 3 && ch {
			t.Fatalf("check %d: transitioned too early", i)
		}
		if i == 3 && (!ch || status != "UP") {
			t.Fatalf("check %d: want UP transition, got status=%q changed=%v", i, status, ch)
		}
	}

	// A single failure does not flip a confirmed UP.
	_, up, down, ch := stepDebounce("UP", up, down, false)
	if ch {
		t.Fatal("one failure must not flip UP")
	}

	// Three failures confirm DOWN.
	status = "UP"
	up, down = 3, 0
	for i := 1; i <= 3; i++ {
		var c bool
		status, up, down, c = stepDebounce(status, up, down, false)
		if i == 3 && (!c || status != "DOWN") {
			t.Fatalf("check %d: want DOWN, got %q changed=%v", i, status, c)
		}
	}
}
