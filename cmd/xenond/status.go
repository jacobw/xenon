package main

import (
	"fmt"

	"xenon/internal/inventory"
	"xenon/internal/metrics"
)

// opStatus is the operational (telemetry-liveness) status shown across the
// monitoring plane — the PRIMARY status, distinct from the engine lifecycle
// state which is a secondary config-health signal.
type opStatus struct {
	Label string // up | unreachable | unknown
	Class string // ok | bad | mut  (CSS pill class)
}

// deviceOp computes one device's operational status from Prometheus liveness
// (stand-in for the real M2.d signal): live series present ⇒ up.
func deviceOp(mc *metrics.Client, source string) opStatus {
	if !mc.Enabled() || !mc.Reachable() {
		return opStatus{"unknown", "mut"}
	}
	if n, _ := mc.Scalar(fmt.Sprintf("count({source=%q})", source)); n > 0 {
		return opStatus{"up", "ok"}
	}
	return opStatus{"unreachable", "bad"}
}

// opFromSeries computes status from a precomputed source→series-count map (so the
// devices list needs one query for the whole fleet).
func opFromSeries(reachable bool, seriesBySource map[string]float64, source string) opStatus {
	if !reachable {
		return opStatus{"unknown", "mut"}
	}
	if seriesBySource[source] > 0 {
		return opStatus{"up", "ok"}
	}
	return opStatus{"unreachable", "bad"}
}

// deviceRow is a devices-list row: the onboarded device + its operational status
// + active alarm count.
type deviceRow struct {
	O      inventory.Onboarded
	Op     opStatus
	Alarms int
}
