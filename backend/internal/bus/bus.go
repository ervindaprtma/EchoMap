// Package bus is the Redis Pub/Sub carrier for real-time events.
package bus

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

const ChannelStatus = "device:status"

// StatusEvent is the `device.status` WS frame (Doc 5 §5.1). The worker publishes it
// to Redis; the API relays the same JSON to browser clients unchanged.
type StatusEvent struct {
	Type       string    `json:"type"`
	DeviceID   int64     `json:"device_id"`
	IPAddress  string    `json:"ip_address"`
	Name       string    `json:"name"`
	FromStatus string    `json:"from_status"`
	ToStatus   string    `json:"to_status"`
	DownReason string    `json:"down_reason,omitempty"` // "PROBE" | "PARENT" while DOWN (Doc 3 §5)
	IsFlapping bool      `json:"is_flapping"`
	LatencyMs  *float64  `json:"latency_ms"`
	PacketLoss *float64  `json:"packet_loss"`
	ChangedAt  time.Time `json:"changed_at"`
}

// UpdatedEvent is the `device.updated` WS frame: an attribute changed without a
// status transition — today only the resolver's IP refresh (Doc 3 §6). It rides
// ChannelStatus so the API relay stays a single subscriber; clients switch on Type.
type UpdatedEvent struct {
	Type      string `json:"type"`
	DeviceID  int64  `json:"device_id"`
	IPAddress string `json:"ip_address"`
	Hostname  string `json:"hostname,omitempty"`
	Name      string `json:"name,omitempty"`
}

type Bus struct {
	rdb *redis.Client
}

func New(url string) (*Bus, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	rdb := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, err
	}
	return &Bus{rdb: rdb}, nil
}

func (b *Bus) PublishStatus(ctx context.Context, ev StatusEvent) error {
	ev.Type = "device.status"
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	return b.rdb.Publish(ctx, ChannelStatus, data).Err()
}

func (b *Bus) PublishUpdated(ctx context.Context, ev UpdatedEvent) error {
	ev.Type = "device.updated"
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	return b.rdb.Publish(ctx, ChannelStatus, data).Err()
}

func (b *Bus) SubscribeStatus(ctx context.Context) *redis.PubSub {
	return b.rdb.Subscribe(ctx, ChannelStatus)
}

func (b *Bus) Close() error { return b.rdb.Close() }
