package telemetry

import (
	"fmt"
	"sort"
	"strings"

	"xenon/internal/model"
)

// GnmicConfig is a simplified gNMIc target configuration (the thing M2.c would
// serve to gNMIc's HTTP loader). Exact schema + label-injection mechanism are
// flagged TBD in docs/m2-telemetry.md; `Tags` represents the intended labels.
type GnmicConfig struct {
	Subscriptions map[string]GnmicSub       `json:"subscriptions"`
	Processors    map[string]GnmicProcessor `json:"processors,omitempty"`
	Targets       map[string]GnmicTarget    `json:"targets"`
}

type GnmicSub struct {
	Paths          []string `json:"paths"`
	StreamMode     string   `json:"stream-mode"`
	SampleInterval string   `json:"sample-interval,omitempty"`
}

// GnmicProcessor is a collector-side transform. event-delete drops metric leaves
// we can't exclude at the subscription path (Junos streams the whole container
// regardless of the requested leaf/subtree). Real gNMIc wires processors on
// outputs; this simplified config attaches them per target.
type GnmicProcessor struct {
	EventDelete EventDelete `json:"event-delete"`
}

type EventDelete struct {
	ValueNames []string `json:"value-names"`
}

type GnmicTarget struct {
	Subscriptions   []string          `json:"subscriptions"`
	EventProcessors []string          `json:"event-processors,omitempty"`
	Username        string            `json:"username"`
	Tags            map[string]string `json:"tags,omitempty"`
}

// Compile turns a device + its assigned profile into a gNMIc target config,
// including base/native subscriptions always and opt-in groups only when the
// device has enabled them.
func Compile(d model.Device, p model.Profile, username string) GnmicConfig {
	cfg := GnmicConfig{
		Subscriptions: map[string]GnmicSub{},
		Processors:    map[string]GnmicProcessor{},
		Targets:       map[string]GnmicTarget{},
	}

	var subNames, procNames []string
	for _, s := range p.Subscriptions {
		if !groupEnabled(s.Group, d.OptIns) {
			continue
		}
		name := subName(s.Path)
		gs := GnmicSub{Paths: []string{s.Path}, StreamMode: s.Mode}
		if s.Mode == "sample" && s.IntervalSec > 0 {
			gs.SampleInterval = fmt.Sprintf("%ds", s.IntervalSec)
		}
		cfg.Subscriptions[name] = gs
		subNames = append(subNames, name)

		// Scope cardinality the only way Junos allows: collector-side. The OC
		// subscription path can't exclude the per-queue out-queue augmentation,
		// so drop it in a gNMIc event-delete processor.
		if len(s.Drop) > 0 {
			pn := "drop_" + name
			cfg.Processors[pn] = GnmicProcessor{EventDelete: EventDelete{ValueNames: s.Drop}}
			procNames = append(procNames, pn)
		}
	}
	sort.Strings(subNames)
	sort.Strings(procNames)

	// Intended metric labels (M2.a): device=hostname, platform, + operator tags.
	tags := map[string]string{"device": d.Hostname, "platform": d.Platform.Model}
	for k, v := range d.Tags {
		tags[k] = v
	}

	cfg.Targets[d.MgmtAddress] = GnmicTarget{
		Subscriptions:   subNames,
		EventProcessors: procNames,
		Username:        username,
		Tags:            tags,
	}
	return cfg
}

// EstimateSeries sums the rough per-path planning estimates for the enabled
// subscriptions — ties the profile to the path-budget method (planning only).
func EstimateSeries(d model.Device, p model.Profile) int {
	total := 0
	for _, s := range p.Subscriptions {
		if groupEnabled(s.Group, d.OptIns) {
			total += s.EstSeries
		}
	}
	return total
}

func groupEnabled(group string, optIns []string) bool {
	switch {
	case group == "" || group == "base" || group == "native":
		return true
	case strings.HasPrefix(group, "optin:"):
		name := strings.TrimPrefix(group, "optin:")
		return contains(optIns, name)
	default:
		return true
	}
}

func subName(path string) string {
	r := strings.NewReplacer("/", "_", "[", "", "]", "", "=", "", "*", "", ":", "_")
	return strings.Trim(r.Replace(path), "_")
}
