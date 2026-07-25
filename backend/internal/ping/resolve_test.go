package ping

import (
	"context"
	"testing"
)

// localhost normally has both a v4 and a v6 record in /etc/hosts, so it proves
// the v4 preference without needing the network. Ping is ip4:icmp-only: picking
// ::1 here would mean a device that can never be probed.
func TestResolve4PrefersIPv4(t *testing.T) {
	got, err := Resolve4(context.Background(), "localhost")
	if err != nil {
		t.Fatalf("Resolve4(localhost): %v", err)
	}
	if got != "127.0.0.1" {
		t.Fatalf("Resolve4(localhost) = %q, want 127.0.0.1", got)
	}
}

func TestResolve4Unresolvable(t *testing.T) {
	if got, err := Resolve4(context.Background(), "nothing-here.invalid"); err == nil {
		t.Fatalf("expected an error, got %q", got)
	}
}
