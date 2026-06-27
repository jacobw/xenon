package main

import "xenon/internal/inventory"

// gnmicTargetOut is the per-target config xenon serves to gnmic's http loader.
// Subscriptions are referenced by name (defined in the static collector config,
// matching the engine's subName scheme); credentials + processors + the
// Prometheus output live in that static config. This is the I3 seam: gnmic pulls
// its target list from xenon's inventory instead of a hand-written config.
type gnmicTargetOut struct {
	Subscriptions []string `json:"subscriptions"`
}

// buildGnmicTargets generates gnmic's target map from inventory, closing the
// inventory → collect loop. Each onboarded device with a compiled config
// contributes a target keyed by its management address.
func buildGnmicTargets(inv *inventory.Store) map[string]gnmicTargetOut {
	out := map[string]gnmicTargetOut{}
	for _, o := range inv.List() {
		for addr, t := range o.Config.Targets {
			out[addr] = gnmicTargetOut{Subscriptions: t.Subscriptions}
		}
	}
	return out
}
