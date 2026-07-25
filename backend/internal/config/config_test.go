package config

import "testing"

func TestLoadDefaultsAndOverride(t *testing.T) {
	t.Setenv("APP_ROLE", "worker")
	t.Setenv("PING_INTERVAL_SECONDS", "25")

	cfg := Load()
	if cfg.AppRole != "worker" {
		t.Fatalf("AppRole = %q, want worker", cfg.AppRole)
	}
	if cfg.PingIntervalSeconds != 25 {
		t.Fatalf("PingIntervalSeconds = %d, want 25", cfg.PingIntervalSeconds)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q, want :8080 (default)", cfg.HTTPAddr)
	}

	// Malformed int must fall back to the default, not panic or zero out.
	t.Setenv("DISCOVERY_CONCURRENCY", "notanumber")
	if got := Load().DiscoveryConcurrency; got != 32 {
		t.Fatalf("DiscoveryConcurrency = %d, want 32 (default on parse error)", got)
	}
}
