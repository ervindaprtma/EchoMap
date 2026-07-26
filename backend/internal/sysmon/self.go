package sysmon

import (
	"context"
	"runtime"
	"time"
)

// SelfStats is one Go role's self-report (Doc 2 §4.9 sys_service).
type SelfStats struct {
	Goroutines  int
	HeapBytes   uint64
	UptimeS     float64
	DBPoolTotal int32
	DBPoolIdle  int32
	DBPingMs    float64
	RedisPingMs float64
}

// DBPinger is satisfied by *store.Store; RedisPinger by *bus.Bus (nil when Redis
// is down). Both probes are cheap (SELECT 1 / PING) on the compose network.
type DBPinger interface {
	PoolStat() (total, idle int32)
	PingDB(ctx context.Context) (float64, error)
}
type RedisPinger interface {
	Ping(ctx context.Context) (float64, error)
}

func ReadSelf(ctx context.Context, start time.Time, db DBPinger, redis RedisPinger) SelfStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	s := SelfStats{
		Goroutines: runtime.NumGoroutine(),
		HeapBytes:  m.HeapAlloc,
		UptimeS:    time.Since(start).Seconds(),
	}
	if db != nil {
		s.DBPoolTotal, s.DBPoolIdle = db.PoolStat()
		if ms, err := db.PingDB(ctx); err == nil {
			s.DBPingMs = ms
		}
	}
	if redis != nil {
		if ms, err := redis.Ping(ctx); err == nil {
			s.RedisPingMs = ms
		}
	}
	return s
}
