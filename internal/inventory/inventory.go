// Package inventory is the prototype M1 inventory: a small in-memory, mutable
// set of devices, each run through engine onboarding (detection -> profile ->
// compile) so the control plane presents one consistent, derived view.
package inventory

import (
	"sort"
	"sync"

	"xenon/internal/content"
	"xenon/internal/model"
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

// seeds is the prototype's starting inventory — example devices that exercise the
// detection paths (exact-match platform, vendor-generic fallback, unknown vendor).
func seeds() []seed {
	return []seed{
		{
			name: "sw1.lab.example.com", mgmt: "192.0.2.10:50051",
			sig: model.Signature{
				Vendor: "Juniper", Model: "EX4100-F-12P", OS: "Junos", Version: "23.4R2-S7.7",
				SupportedModels: []string{"openconfig-interfaces", "openconfig-system", "openconfig-platform"},
			},
			tags: map[string]string{"site": "lab", "role": "access"},
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

// Store is the in-memory M1 inventory (prototype): seeded at construction and
// mutable via Add (onboarding).
type Store struct {
	mu      sync.RWMutex
	content *content.Store
	devices []Onboarded
}

// NewStore builds a store seeded with the starting devices.
func NewStore(c *content.Store) *Store {
	s := &Store{content: c}
	for _, sd := range seeds() {
		s.devices = append(s.devices, onboard(sd, c))
	}
	return s
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

// Add stores an onboarded device, replacing any existing one with the same id.
func (s *Store) Add(o Onboarded) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.devices {
		if s.devices[i].Device.ID == o.Device.ID {
			s.devices[i] = o
			return
		}
	}
	s.devices = append(s.devices, o)
}
