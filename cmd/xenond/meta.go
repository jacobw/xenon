package main

import (
	"sync"
	"time"

	"xenon/internal/inventory"
	"xenon/internal/probe"
)

// metaStore caches per-device descriptions (keyed by mgmt address). Static config
// leaves don't survive the Prometheus scrape, so xenond captures them out-of-band
// via gNMI ONCE and joins them at render — the inventory-metadata pattern.
type metaStore struct {
	mu    sync.RWMutex
	byDev map[string]probe.Meta
	creds probe.Creds
}

func newMetaStore(creds probe.Creds) *metaStore {
	return &metaStore{byDev: map[string]probe.Meta{}, creds: creds}
}

func (s *metaStore) get(addr string) probe.Meta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byDev[addr]
}

func (s *metaStore) set(addr string, m probe.Meta) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byDev[addr] = m
}

// capture refreshes one device's descriptions (best-effort; keeps prior cache on
// error). Safe to call in a goroutine after onboarding.
func (s *metaStore) capture(addr string) {
	if m, err := probe.Descriptions(addr, s.creds); err == nil {
		s.set(addr, m)
	}
}

// run captures all inventory devices immediately, then on the given interval.
// Captures run concurrently so an unreachable device's timeout doesn't block others.
func (s *metaStore) run(inv *inventory.Store, interval time.Duration) {
	refresh := func() {
		var wg sync.WaitGroup
		for _, o := range inv.List() {
			wg.Add(1)
			go func(addr string) { defer wg.Done(); s.capture(addr) }(o.Device.MgmtAddress)
		}
		wg.Wait()
	}
	refresh()
	for range time.Tick(interval) {
		refresh()
	}
}
