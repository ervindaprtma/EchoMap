// Package config loads runtime settings from the environment with sane defaults.
// Secrets are read here but never logged.
package config

import (
	"os"
	"strconv"
)

type Config struct {
	AppRole  string
	HTTPAddr string

	DatabaseURL  string
	RedisURL     string
	InfluxURL    string
	InfluxOrg    string
	InfluxBucket string
	InfluxToken  string

	NetboxURL   string
	NetboxToken string

	EncryptionKey string // APP_ENCRYPTION_KEY — AES-256-GCM key for secrets at rest
	APIToken      string // APP_API_TOKEN — static bearer token for headless automation
	AdminPassword string // APP_ADMIN_PASSWORD — seeds the bootstrap Superadmin on an empty users table (Phase 8)

	PingIntervalSeconds  int
	DiscoveryConcurrency int
}

func Load() Config {
	return Config{
		AppRole:              env("APP_ROLE", "api"),
		HTTPAddr:             env("HTTP_ADDR", ":8080"),
		DatabaseURL:          env("DATABASE_URL", "postgres://echomap:echomap@localhost:5432/echomap?sslmode=disable"),
		RedisURL:             env("REDIS_URL", "redis://localhost:6379/0"),
		InfluxURL:            env("INFLUX_URL", "http://localhost:8086"),
		InfluxOrg:            env("INFLUX_ORG", "echomap"),
		InfluxBucket:         env("INFLUX_BUCKET", "ping_metrics"),
		InfluxToken:          env("INFLUX_TOKEN", ""),
		NetboxURL:            env("NETBOX_URL", ""),
		NetboxToken:          env("NETBOX_TOKEN", ""),
		EncryptionKey:        env("APP_ENCRYPTION_KEY", ""),
		APIToken:             env("APP_API_TOKEN", ""),
		AdminPassword:        env("APP_ADMIN_PASSWORD", ""),
		PingIntervalSeconds:  envInt("PING_INTERVAL_SECONDS", 20),
		DiscoveryConcurrency: envInt("DISCOVERY_CONCURRENCY", 32),
	}
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
