// Package inventory is the prototype M1 inventory: a small in-memory, mutable
// set of devices, each run through engine onboarding (detection -> profile ->
// compile) so the control plane presents one consistent, derived view.
package inventory

import (
	"sort"
	"sync"

	"xenon/internal/content"
	"xenon/internal/model"
	"xenon/internal/persist"
	"xenon/internal/telemetry"
)

// Onboarded is a device plus everything the engine derived for it: the detection
// result, the assigned profile, the planning cardinality, and the compiled
// gNMIc target config.
type Onboarded struct {
	Device    model.Device
	Signature model.Signature
	Rule      model.DetectionRule
	Matched   bool
	Profile   model.Profile
	EstSeries int
	Config    telemetry.GnmicConfig
}

type seed struct {
	name   string
	mgmt   string
	sig    model.Signature
	optIns []string
	tags   map[string]string
}

func (s seed) record() persist.DeviceRecord {
	return persist.DeviceRecord{Name: s.name, Mgmt: s.mgmt, Sig: s.sig, OptIns: s.optIns, Tags: s.tags}
}

func seedOf(r persist.DeviceRecord) seed {
	return seed{name: r.Name, mgmt: r.Mgmt, sig: r.Sig, optIns: r.OptIns, tags: r.Tags}
}

// seeds is the prototype's starting inventory — example devices that exercise the
// detection paths (exact-match platform, vendor-generic fallback, unknown vendor).
func seeds() []seed {
	return []seed{
		{
			name: "switch1.example.com", mgmt: "192.0.2.10:50051",
			sig: model.Signature{
				Vendor: "Juniper", Model: "EX4100-F-12P", OS: "Junos", Version: "23.4R2-S7.7",
				SupportedModels: []string{"openconfig-interfaces", "openconfig-system", "openconfig-platform"},
			},
			tags: map[string]string{"site": "dc1", "role": "access"},
		},
		{
			name: "core1.example.com", mgmt: "core1.example.com:9339",
			sig: model.Signature{
				Vendor: "Juniper Networks", Model: "MX304", OS: "Junos", Version: "23.4R1.9",
				SupportedModels: []string{"openconfig-interfaces", "openconfig-system", "openconfig-network-instance"},
			},
			optIns: []string{"qos"},
			tags:   map[string]string{"site": "dc1", "role": "core"},
		},
		{
			name: "mystery1", mgmt: "mystery1:9339",
			sig: model.Signature{
				Vendor: "Acme", Model: "X9000", OS: "AcmeOS", Version: "1.0",
				SupportedModels: []string{"openconfig-interfaces", "openconfig-system"},
			},
		},
	}
}

func onboard(s seed, store *content.Store) Onboarded {
	o := Onboarded{Signature: s.sig}
	rule, ok := telemetry.Detect(s.sig, store.DetectionRules)
	o.Rule, o.Matched = rule, ok

	state := "unclassified"
	if ok && rule.Platform.Model != "unknown" {
		state = "active"
	}

	dev := model.Device{
		ID: "d-" + s.name, Hostname: s.name, MgmtAddress: s.mgmt,
		CredentialRef: "telemetry-ro", Tags: s.tags,
		OptIns: s.optIns, Platform: rule.Platform, State: state,
	}
	if ok {
		if prof, exists := store.Profiles[rule.Profile]; exists {
			dev.ProfileID = prof.ID
			o.Profile = prof
			o.EstSeries = telemetry.EstimateSeries(dev, prof)
			o.Config = telemetry.Compile(dev, prof, "telemetry-ro")
		}
	}
	o.Device = dev
	return o
}

// Store is the M1 inventory: an in-memory view kept consistent with a persistent
// device store (SQLite). It is mutable via Add (onboarding).
type Store struct {
	mu      sync.RWMutex
	content *content.Store
	db      *persist.Store
	devices []Onboarded
}

// NewStore builds the inventory from the persistent store db (which may be nil for
// an ephemeral, in-memory store, e.g. in tests). On first run (empty db) it seeds
// the example devices and persists them; thereafter it loads whatever was onboarded.
func NewStore(c *content.Store, db *persist.Store) (*Store, error) {
	s := &Store{content: c, db: db}

	var records []persist.DeviceRecord
	if db != nil {
		n, err := db.Count()
		if err != nil {
			return nil, err
		}
		if n == 0 {
			for _, sd := range seeds() {
				if err := db.Upsert(sd.record()); err != nil {
					return nil, err
				}
			}
		}
		if records, err = db.List(); err != nil {
			return nil, err
		}
	} else {
		for _, sd := range seeds() {
			records = append(records, sd.record())
		}
	}

	for _, r := range records {
		s.devices = append(s.devices, onboard(seedOf(r), c))
	}
	return s, nil
}

// List returns the inventory ordered by hostname.
func (s *Store) List() []Onboarded {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Onboarded, len(s.devices))
	copy(out, s.devices)
	sort.Slice(out, func(i, j int) bool { return out[i].Device.Hostname < out[j].Device.Hostname })
	return out
}

// Get returns the onboarded device with the given id ("d-<hostname>").
func (s *Store) Get(id string) (Onboarded, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, o := range s.devices {
		if o.Device.ID == id {
			return o, true
		}
	}
	return Onboarded{}, false
}

// Preview onboards a device from a signature WITHOUT storing it (dry run for the
// onboarding detect step).
func (s *Store) Preview(sig model.Signature, host, mgmt string) Onboarded {
	return onboard(seed{name: host, mgmt: mgmt, sig: sig}, s.content)
}

// Add persists an onboarded device and updates the in-memory view, replacing any
// existing one with the same id. Persistence happens first so a write failure
// surfaces instead of silently losing the device on the next restart.
func (s *Store) Add(o Onboarded) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db != nil {
		rec := persist.DeviceRecord{
			Name: o.Device.Hostname, Mgmt: o.Device.MgmtAddress,
			Sig: o.Signature, OptIns: o.Device.OptIns, Tags: o.Device.Tags,
		}
		if err := s.db.Upsert(rec); err != nil {
			return err
		}
	}

	for i := range s.devices {
		if s.devices[i].Device.ID == o.Device.ID {
			s.devices[i] = o
			return nil
		}
	}
	s.devices = append(s.devices, o)
	return nil
}
