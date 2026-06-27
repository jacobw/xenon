// Package content loads bundled content (detection rules, profiles) and is the
// home of the future bundled+overlay merge engine (M5). Prototype: bundled only.
package content

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"

	"xenon/internal/model"
)

//go:embed bundled/*.json
var bundledFS embed.FS

// Store holds the resolved (here: bundled-only) content set.
type Store struct {
	DetectionRules []model.DetectionRule
	Profiles       map[string]model.Profile
	Alerts         []model.AlertRule
}

// LoadBundled reads the embedded bundled content and returns a Store with
// detection rules sorted highest-priority-first.
func LoadBundled() (*Store, error) {
	s := &Store{Profiles: map[string]model.Profile{}}

	rb, err := bundledFS.ReadFile("bundled/detection.json")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(rb, &s.DetectionRules); err != nil {
		return nil, fmt.Errorf("detection.json: %w", err)
	}

	pb, err := bundledFS.ReadFile("bundled/profiles.json")
	if err != nil {
		return nil, err
	}
	var profs []model.Profile
	if err := json.Unmarshal(pb, &profs); err != nil {
		return nil, fmt.Errorf("profiles.json: %w", err)
	}
	for _, p := range profs {
		s.Profiles[p.ID] = p
	}

	if ab, err := bundledFS.ReadFile("bundled/alerts.json"); err == nil {
		if err := json.Unmarshal(ab, &s.Alerts); err != nil {
			return nil, fmt.Errorf("alerts.json: %w", err)
		}
	}

	sort.SliceStable(s.DetectionRules, func(i, j int) bool {
		return s.DetectionRules[i].Priority > s.DetectionRules[j].Priority
	})
	return s, nil
}
