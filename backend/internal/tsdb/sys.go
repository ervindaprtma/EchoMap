package tsdb

import (
	"context"
	"fmt"
	"time"
)

// SysServiceLatest is the newest sys_service point for one service tag.
type SysServiceLatest struct {
	Fields map[string]float64
	At     time.Time
}

// SysLatest returns the newest sys_host fields and the newest sys_service point
// per service, for the overview snapshot. Empty maps when Influx is unconfigured.
func (r *Reader) SysLatest(ctx context.Context) (host map[string]float64, hostAt time.Time, services map[string]SysServiceLatest, err error) {
	host = map[string]float64{}
	services = map[string]SysServiceLatest{}
	if r.query == nil {
		return host, hostAt, services, nil
	}
	flux := fmt.Sprintf(`from(bucket: %q)
  |> range(start: -10m)
  |> filter(fn: (r) => r._measurement == "sys_host" or r._measurement == "sys_service")
  |> last()`, r.bucket)
	res, err := r.query.Query(ctx, flux)
	if err != nil {
		return host, hostAt, services, err
	}
	for res.Next() {
		rec := res.Record()
		val, ok := toFloat(rec.Value())
		if !ok {
			continue
		}
		if rec.Measurement() == "sys_host" {
			host[rec.Field()] = val
			if rec.Time().After(hostAt) {
				hostAt = rec.Time()
			}
			continue
		}
		svc, _ := rec.ValueByKey("service").(string)
		s, ok := services[svc]
		if !ok {
			s = SysServiceLatest{Fields: map[string]float64{}}
		}
		s.Fields[rec.Field()] = val
		if rec.Time().After(s.At) {
			s.At = rec.Time()
		}
		services[svc] = s
	}
	return host, hostAt, services, res.Err()
}

// SysPoint is one aggregation window of a sys_* series; every numeric field is
// carried, nil for an empty window.
type SysPoint struct {
	T      time.Time          `json:"t"`
	Fields map[string]float64 `json:"-"`
}

// SysHistory reads a ranged series for one target. tag/tagVal are "" for host.
func (r *Reader) SysHistory(ctx context.Context, measurement, tag, tagVal string, rg Range) ([]SysPoint, error) {
	if r.query == nil {
		return []SysPoint{}, nil
	}
	tagFilter := ""
	if tag != "" {
		tagFilter = fmt.Sprintf(` and r.%s == %q`, tag, tagVal)
	}
	flux := fmt.Sprintf(`from(bucket: %q)
  |> range(start: %s)
  |> filter(fn: (r) => r._measurement == %q%s)
  |> aggregateWindow(every: %s, fn: mean, createEmpty: false)
  |> pivot(rowKey: ["_time"], columnKey: ["_field"], valueColumn: "_value")
  |> sort(columns: ["_time"])`, r.bucket, rg.Start, measurement, tagFilter, rg.Window)
	res, err := r.query.Query(ctx, flux)
	if err != nil {
		return nil, err
	}
	out := []SysPoint{}
	for res.Next() {
		rec := res.Record()
		p := SysPoint{T: rec.Time(), Fields: map[string]float64{}}
		for k, v := range rec.Values() {
			if k == "_time" || k == "_measurement" || k == "_start" || k == "_stop" || k == tag {
				continue
			}
			if f, ok := toFloat(v); ok {
				p.Fields[k] = f
			}
		}
		out = append(out, p)
	}
	return out, res.Err()
}

// toFloat coerces an InfluxDB numeric cell (float64/int64) to float64.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
