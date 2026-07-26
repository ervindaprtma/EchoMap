package tsdb

import (
	"math"
	"testing"
	"time"
)

func TestParseRange(t *testing.T) {
	if got := ParseRange("7d"); got.Key != "7d" || got.Window != "30m" {
		t.Fatalf("7d: got %+v", got)
	}
	// Empty and unknown both fall back to 24h.
	for _, in := range []string{"", "bogus", "1y"} {
		if got := ParseRange(in); got.Key != "24h" {
			t.Fatalf("%q: want 24h default, got %s", in, got.Key)
		}
	}
}

func TestDeriveJitter(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	pts := []PingPoint{
		{T: time.Unix(0, 0), LatencyAvg: f(10)},
		{T: time.Unix(1, 0), LatencyAvg: f(20)}, // |20-10|=10 → J = 0 + (10-0)/16
		{T: time.Unix(2, 0), LatencyAvg: nil},   // gap: no jitter, resets prev
		{T: time.Unix(3, 0), LatencyAvg: f(30)}, // prev reset → still no jitter
		{T: time.Unix(4, 0), LatencyAvg: f(30)}, // |30-30|=0 → J decays toward 0
	}
	deriveJitter(pts)

	if pts[0].Jitter != nil {
		t.Fatalf("first point has no predecessor, want nil jitter")
	}
	if got := *pts[1].Jitter; math.Abs(got-10.0/16) > 1e-9 {
		t.Fatalf("point 1 jitter: want %v got %v", 10.0/16, got)
	}
	if pts[2].Jitter != nil || pts[3].Jitter != nil {
		t.Fatalf("gap must break the running estimate (points 2,3 nil)")
	}
	// After a zero delta, jitter decays: J = prevJ + (0 - prevJ)/16 < prevJ.
	if got := *pts[4].Jitter; got <= 0 || got >= 10.0/16 {
		t.Fatalf("point 4 jitter should decay into (0, %v), got %v", 10.0/16, got)
	}
}
