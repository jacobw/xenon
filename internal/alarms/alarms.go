// Package alarms is the prototype M3 app-native alarm engine: it evaluates
// content alert rules (PromQL) against the metric store on a loop and maintains
// alarm lifecycle (active / cleared / acked) in memory. App-native (not
// Prometheus-ruler+Alertmanager) so the NMS owns the lifecycle; tradeoff is that
// evaluation stops if the app is down.
package alarms

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"xenon/internal/metrics"
	"xenon/internal/model"
)

// Alarm is one alarm instance: identity = rule + device + entity.
type Alarm struct {
	Key       string
	RuleID    string
	Severity  string
	Summary   string
	Device    string
	Entity    string
	Value     float64
	State     string // active | cleared
	Acked     bool
	FirstSeen time.Time
	LastSeen  time.Time
}

// Store holds the rules and the live alarm set.
type Store struct {
	mu     sync.RWMutex
	rules  []model.AlertRule
	alarms map[string]*Alarm
}

func NewStore(rules []model.AlertRule) *Store {
	return &Store{rules: rules, alarms: map[string]*Alarm{}}
}

func (s *Store) Rules() []model.AlertRule { return s.rules }

// Run evaluates immediately then every interval (call in a goroutine).
func (s *Store) Run(mc *metrics.Client, interval time.Duration) {
	s.Evaluate(mc)
	for range time.Tick(interval) {
		s.Evaluate(mc)
	}
}

// Evaluate runs one cycle: query each rule (outside the lock), then update
// lifecycle (firing series → active; previously-firing, now absent → cleared).
func (s *Store) Evaluate(mc *metrics.Client) {
	if !mc.Reachable() {
		return
	}
	type res struct {
		rule   model.AlertRule
		series []metrics.Labeled
	}
	results := make([]res, 0, len(s.rules))
	for _, rule := range s.rules {
		results = append(results, res{rule, mc.VectorFull(rule.Expr)})
	}

	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range results {
		fired := map[string]bool{}
		for _, ls := range r.series {
			device := ls.Labels["device"]
			if device == "" {
				device = ls.Labels["source"]
			}
			entity := first(ls.Labels, "interface_name", "component_name")
			key := r.rule.ID + "|" + device + "|" + entity
			fired[key] = true
			a := s.alarms[key]
			if a == nil {
				a = &Alarm{Key: key, RuleID: r.rule.ID, Severity: r.rule.Severity, Device: device, Entity: entity, FirstSeen: now}
				s.alarms[key] = a
			}
			a.Value = ls.Val
			a.Summary = renderSummary(r.rule.Summary, ls.Labels, ls.Val)
			a.LastSeen = now
			if !a.Acked {
				a.State = "active"
			}
		}
		for key, a := range s.alarms {
			if a.RuleID == r.rule.ID && !fired[key] {
				a.State = "cleared"
				a.Acked = false
				a.LastSeen = now
			}
		}
	}
}

func (s *Store) Ack(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a := s.alarms[key]; a != nil && a.State == "active" {
		a.Acked = true
	}
}

func (s *Store) Active() []Alarm {
	return s.snapshot(func(a *Alarm) bool { return a.State == "active" })
}

func (s *Store) Cleared() []Alarm {
	cl := s.snapshot(func(a *Alarm) bool { return a.State == "cleared" })
	if len(cl) > 10 {
		cl = cl[:10]
	}
	return cl
}

func (s *Store) ForDevice(device string) []Alarm {
	return s.snapshot(func(a *Alarm) bool { return a.State == "active" && a.Device == device })
}

func (s *Store) Counts() (crit, warn int) {
	for _, a := range s.Active() {
		if a.Severity == "critical" {
			crit++
		} else {
			warn++
		}
	}
	return
}

func (s *Store) CountsByDevice() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := map[string]int{}
	for _, a := range s.alarms {
		if a.State == "active" {
			m[a.Device]++
		}
	}
	return m
}

func (s *Store) snapshot(keep func(*Alarm) bool) []Alarm {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Alarm
	for _, a := range s.alarms {
		if keep(a) {
			out = append(out, *a)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if ri, rj := sevRank(out[i].Severity), sevRank(out[j].Severity); ri != rj {
			return ri < rj
		}
		return out[i].FirstSeen.Before(out[j].FirstSeen)
	})
	return out
}

func sevRank(sev string) int {
	switch sev {
	case "critical":
		return 0
	case "warning":
		return 1
	default:
		return 2
	}
}

func first(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := m[k]; v != "" {
			return v
		}
	}
	return ""
}

func renderSummary(tmpl string, labels map[string]string, value float64) string {
	out := tmpl
	for k, v := range labels {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	return strings.ReplaceAll(out, "{{value}}", strconv.FormatFloat(value, 'f', -1, 64))
}
