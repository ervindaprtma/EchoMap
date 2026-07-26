package notify

import (
	"context"

	"echomap/internal/monitor"
)

// Channel is one alert delivery mechanism (Telegram, Email, …). Both methods
// re-read their own config per alert and no-op when that channel isn't
// configured, so a Multi never needs an "is X enabled?" flag.
type Channel interface {
	StatusAlert(ctx context.Context, d monitor.Device, from, to string, affected []monitor.Child)
	MonitorAlert(ctx context.Context, deviceID int64, label, kind, from, to string)
	FlappingAlert(ctx context.Context, d monitor.Device)
}

// Multi fans an alert out to every channel. It satisfies monitor.Alerter
// (StatusAlert) and the monitor scheduler's MonitorAlert. Delivery is
// sequential — alerts are rare, so a slow channel briefly delaying the next is
// acceptable; goroutine the loop if that ever changes.
type Multi struct{ channels []Channel }

func NewMulti(channels ...Channel) *Multi { return &Multi{channels: channels} }

func (m *Multi) StatusAlert(ctx context.Context, d monitor.Device, from, to string, affected []monitor.Child) {
	for _, c := range m.channels {
		c.StatusAlert(ctx, d, from, to, affected)
	}
}

func (m *Multi) MonitorAlert(ctx context.Context, deviceID int64, label, kind, from, to string) {
	for _, c := range m.channels {
		c.MonitorAlert(ctx, deviceID, label, kind, from, to)
	}
}

func (m *Multi) FlappingAlert(ctx context.Context, d monitor.Device) {
	for _, c := range m.channels {
		c.FlappingAlert(ctx, d)
	}
}
