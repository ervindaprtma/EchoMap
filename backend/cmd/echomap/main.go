// Command echomap is the single EchoMap binary. It boots into one role
// (api | worker) selected by --role or APP_ROLE, per Doc 1.
package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"

	"echomap/internal/api"
	"echomap/internal/bus"
	"echomap/internal/config"
	"echomap/internal/db"
	"echomap/internal/secrets"
	"echomap/internal/store"
	"echomap/internal/tsdb"
	"echomap/internal/worker"
)

func main() {
	role := flag.String("role", "", "process role: api | worker (overrides APP_ROLE)")
	flag.Parse()

	cfg := config.Load()
	if *role != "" {
		cfg.AppRole = *role
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Security first: secrets at rest are AES-256-GCM sealed, so refusing to
	// boot without the key beats silently storing/reading plaintext.
	box, err := secrets.New(cfg.EncryptionKey)
	if err != nil {
		log.Fatalf("APP_ENCRYPTION_KEY is required (%v) — set it in .env; see README §Required environment variables", err)
	}

	log.Printf("echomap starting role=%s", cfg.AppRole)

	switch cfg.AppRole {
	case "api":
		pool, err := db.Connect(ctx, cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("db connect: %v", err)
		}
		defer pool.Close()

		b := dialBus(cfg.RedisURL) // nil if unavailable: WS just gets no events
		if b != nil {
			defer b.Close()
		}
		if err := api.Run(ctx, cfg, store.New(pool, box), b); err != nil {
			log.Fatalf("role api exited: %v", err)
		}

	case "worker":
		pool, err := db.Connect(ctx, cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("db connect: %v", err)
		}
		defer pool.Close()

		b := dialBus(cfg.RedisURL)
		if b != nil {
			defer b.Close()
		}
		tw := tsdb.New(cfg.InfluxURL, cfg.InfluxToken, cfg.InfluxOrg, cfg.InfluxBucket)
		defer tw.Close()

		if err := worker.Run(ctx, cfg, store.New(pool, box), b, tw); err != nil {
			log.Fatalf("role worker exited: %v", err)
		}

	default:
		log.Fatalf("unknown role %q (want: api | worker)", cfg.AppRole)
	}

	log.Printf("echomap role=%s stopped cleanly", cfg.AppRole)
}

func dialBus(url string) *bus.Bus {
	b, err := bus.New(url)
	if err != nil {
		log.Printf("warning: redis unavailable, real-time events disabled: %v", err)
		return nil
	}
	return b
}
