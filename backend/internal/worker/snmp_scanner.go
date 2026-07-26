package worker

import (
	"context"
	"log"
	"sync"
	"time"

	"echomap/internal/snmp"
	"echomap/internal/store"
)

const (
	snmpScanInterval = 30 * time.Second
	snmpTimeout      = 2 * time.Second
	snmpBatchLimit   = 64 // devices fingerprinted per tick
)

// runSNMPScanner fingerprints SNMP-enabled devices that have never been
// fingerprinted (Slice 4). It reuses the settings-level community; if none is
// configured it no-ops, mirroring the "inert when unconfigured" pattern. This is
// also the discovery hand-off — a discovered device with snmp_enabled=true gets
// picked up here (Doc 3 §1 step 5).
func runSNMPScanner(ctx context.Context, st *store.Store, concurrency int) {
	if concurrency < 1 {
		concurrency = 8
	}
	tick := time.NewTicker(snmpScanInterval)
	defer tick.Stop()

	scan := func() {
		version, community, err := st.SNMPCredentials(ctx)
		if err != nil || community == "" {
			return // SNMP not configured (or settings unreadable) — nothing to do
		}
		targets, err := st.DevicesNeedingFingerprint(ctx, snmpBatchLimit)
		if err != nil || len(targets) == 0 {
			return
		}

		sem := make(chan struct{}, concurrency)
		var wg sync.WaitGroup
		for _, t := range targets {
			wg.Add(1)
			sem <- struct{}{}
			go func(t store.FingerprintTarget) {
				defer wg.Done()
				defer func() { <-sem }()

				res, err := snmp.Fingerprint(ctx, t.IP, version, community, snmpTimeout)
				if err != nil {
					// ponytail: unreachable/timeout leaves sys_descr NULL, so the next
					// tick retries. That's intentional; no failure column needed.
					return
				}
				if err := st.SaveFingerprint(ctx, t.ID, res); err != nil {
					log.Printf("snmp: save fingerprint device=%d: %v", t.ID, err)
					return
				}
				st.Info(ctx, "config", "SNMP fingerprint: "+res.Vendor+" "+res.Model+" ("+res.SysName+")", &t.ID)
			}(t)
		}
		wg.Wait()
	}

	scan() // boot sweep
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			scan()
		}
	}
}
