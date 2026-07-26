package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"echomap/internal/bus"
	"echomap/internal/ping"
)

const (
	maxSweepHostBits = 16          // reject anything wider than a /16 (bounds memory + the ICMP id space)
	sweepPingTimeout = time.Second // per-IP ICMP timeout; skip-on-fail, no retry (Doc 3 §1)
)

// batchStatus is the in-memory record backing the polling fallback (GET .../status)
// and the WS progress frames.
//
// ponytail: in-process state, no durable queue. A subnet sweep is idempotent and
// re-runnable (skip-on-fail creates no partial harm), the api role already holds
// NET_RAW for the ping tools, and Redis already carries progress — so asynq would
// only buy durability/retries we don't need. Upgrade to asynq if sweeps must
// survive an api restart or run across multiple api replicas.
type batchStatus struct {
	BatchID   string `json:"batch_id"`
	Status    string `json:"status"` // QUEUED | RUNNING | DONE
	Total     int    `json:"total"`
	Processed int    `json:"processed"`
	Added     int    `json:"added"`
	Skipped   int    `json:"skipped"`
	CurrentIP string `json:"current_ip"`
}

// discoverSubnet expands a CIDR, launches a background skip-on-fail ICMP sweep, and
// returns 202 immediately with a batch id the client polls (or watches over WS).
func (s *server) discoverSubnet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CIDR        string `json:"cidr"`
		Site        string `json:"site"`
		SNMPEnabled bool   `json:"snmp_enabled"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	ips, err := expandCIDR(strings.TrimSpace(body.CIDR))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}

	// Group the discovered devices under a subnet row so they inherit its site.
	// A subnet failure is non-fatal: the devices are still created, just unsited.
	var subnetID *int64
	if id, err := s.store.UpsertSubnet(r.Context(), body.CIDR, strings.TrimSpace(body.Site)); err == nil {
		subnetID = &id
	}

	batch := &batchStatus{BatchID: newBatchID(), Status: "QUEUED", Total: len(ips)}
	s.batchMu.Lock()
	s.batches[batch.BatchID] = batch
	s.batchMu.Unlock()

	go s.runSubnetSweep(batch, ips, subnetID, body.SNMPEnabled)

	s.logEvent(r, "NOTICE", "config", fmt.Sprintf("subnet discovery started for %s (%d hosts)", body.CIDR, len(ips)), nil, nil)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"batch_id": batch.BatchID, "total_estimated": len(ips), "status": "QUEUED",
	})
}

func (s *server) discoverStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("batch_id")
	s.batchMu.Lock()
	b, ok := s.batches[id]
	var snapshot batchStatus
	if ok {
		snapshot = *b
	}
	s.batchMu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown batch_id"})
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

// runSubnetSweep pings every IP on a bounded pool, skips the dead ones immediately
// (Doc 3 §1), upserts the alive ones as UP devices, and streams discovery.progress
// frames over Redis → WS. It detaches from the request so the scan can outlive it.
func (s *server) runSubnetSweep(batch *batchStatus, ips []string, subnetID *int64, snmp bool) {
	ctx := context.Background()

	concurrency := s.cfg.DiscoveryConcurrency
	if concurrency < 1 {
		concurrency = 32
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	s.batchMu.Lock()
	batch.Status = "RUNNING"
	s.batchMu.Unlock()

	for _, ip := range ips {
		wg.Add(1)
		sem <- struct{}{}
		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }()

			alive, _, _ := ping.Ping(ip, sweepPingTimeout)
			action := "skipped"
			if alive {
				// created=false ⇒ already monitored: still alive, but not "added".
				if created, err := s.store.UpsertDiscoveredDevice(ctx, ip, subnetID, snmp); err == nil && created {
					action = "added"
				} else {
					action = "exists"
				}
			}

			s.batchMu.Lock()
			batch.Processed++
			switch action {
			case "added":
				batch.Added++
			case "skipped":
				batch.Skipped++
			}
			batch.CurrentIP = ip
			snap := *batch
			s.batchMu.Unlock()

			if s.bus != nil {
				_ = s.bus.PublishDiscovery(ctx, bus.DiscoveryEvent{
					Type: "discovery.progress", BatchID: snap.BatchID,
					Processed: snap.Processed, Total: snap.Total,
					Added: snap.Added, Skipped: snap.Skipped,
					CurrentIP: ip, Action: action,
				})
			}
		}(ip)
	}
	wg.Wait()

	s.batchMu.Lock()
	batch.Status = "DONE"
	snap := *batch
	s.batchMu.Unlock()

	if s.bus != nil {
		_ = s.bus.PublishDiscovery(ctx, bus.DiscoveryEvent{
			Type: "discovery.done", BatchID: snap.BatchID,
			Processed: snap.Processed, Total: snap.Total,
			Added: snap.Added, Skipped: snap.Skipped,
		})
	}
	// ponytail: the Slice-4 SNMP fingerprint hand-off for the added devices goes
	// here once that worker exists.
}

// expandCIDR returns the pingable host IPs of an IPv4 CIDR, excluding the network
// and broadcast addresses for /30 and shorter; /31 keeps both (RFC 3021 p2p) and
// /32 keeps the single host. Rejects non-IPv4 and oversized ranges.
func expandCIDR(cidr string) ([]string, error) {
	p, err := netip.ParsePrefix(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR: %q", cidr)
	}
	p = p.Masked()
	if !p.Addr().Is4() {
		return nil, fmt.Errorf("only IPv4 subnets are supported")
	}
	hostBits := 32 - p.Bits()
	if hostBits > maxSweepHostBits {
		return nil, fmt.Errorf("subnet too large (max /%d)", 32-maxSweepHostBits)
	}
	count := 1 << hostBits
	skipEnds := hostBits >= 2 // /30 and shorter carry a network + broadcast address

	ips := make([]string, 0, count)
	addr := p.Addr()
	for i := 0; i < count; i++ {
		if !(skipEnds && (i == 0 || i == count-1)) {
			ips = append(ips, addr.String())
		}
		addr = addr.Next()
	}
	return ips, nil
}

func newBatchID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return "b_" + hex.EncodeToString(b[:])
}
