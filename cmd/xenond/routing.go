package main

import (
	"fmt"
	"sort"

	"xenon/internal/metrics"
	"xenon/internal/probe"
)

// bgpPeer is one BGP neighbour row for the Routing tab.
type bgpPeer struct {
	Neighbor    string
	Desc        string // neighbour description (from app metadata)
	Network     string // network-instance (VRF) name
	PeerAS      string
	State       string // ESTABLISHED / ACTIVE / IDLE / …
	StateClass  string // ok | warn | bad
	PfxRecv     string // prefixes received (summed over afi-safis)
	PfxSent     string // prefixes sent
	Transitions string // established-transitions (flap count)
}

// bgpBase / pfxBase are the (long) metric-name prefixes for OpenConfig BGP
// neighbour state and per-afi-safi prefix counts.
const (
	bgpBase = "network_instances_network_instance_protocols_protocol_bgp_neighbors_neighbor_state_"
	pfxBase = "network_instances_network_instance_protocols_protocol_bgp_neighbors_neighbor_afi_safis_afi_safi_state_prefixes_"
)

// bgpStateClass colours a BGP session state: established = good, idle = down,
// in-between (active/connect/opensent/openconfirm) = warning.
func bgpStateClass(s string) string {
	switch s {
	case "ESTABLISHED":
		return "ok"
	case "IDLE", "":
		return "bad"
	default:
		return "warn"
	}
}

// buildRouting reads BGP neighbours for a device from the state-set session-state
// metric (strings-as-labels), joined with numeric peer-as and flap count.
func buildRouting(mc *metrics.Client, source string, meta probe.Meta) []bgpPeer {
	peerAS := mc.VectorBy(fmt.Sprintf(bgpBase+`peer_as{source=%q}`, source), "neighbor_neighbor_address")
	trans := mc.VectorBy(fmt.Sprintf(bgpBase+`established_transitions{source=%q}`, source), "neighbor_neighbor_address")
	// Prefix COUNTS only (a gauge per neighbour) — summed across afi-safis. The
	// actual prefixes (the RIB) are deliberately NOT in Prometheus: per-route series
	// would be a cardinality blow-up and a TSDB is the wrong store for a route table.
	recv := mc.VectorBy(fmt.Sprintf(`sum by (neighbor_neighbor_address)(`+pfxBase+`received{source=%q})`, source), "neighbor_neighbor_address")
	sent := mc.VectorBy(fmt.Sprintf(`sum by (neighbor_neighbor_address)(`+pfxBase+`sent{source=%q})`, source), "neighbor_neighbor_address")

	pfx := func(m map[string]float64, nb string) string {
		if v, ok := m[nb]; ok {
			return fmt.Sprintf("%.0f", v)
		}
		return "—"
	}

	var peers []bgpPeer
	for _, l := range mc.VectorFull(fmt.Sprintf(bgpBase+`session_state{source=%q}`, source)) {
		nb := l.Labels["neighbor_neighbor_address"]
		if nb == "" {
			continue
		}
		p := bgpPeer{
			Neighbor:   nb,
			Desc:       meta.BGP[nb],
			Network:    l.Labels["network_instance_name"],
			State:      l.Labels["session_state"],
			StateClass: bgpStateClass(l.Labels["session_state"]),
			PfxRecv:    pfx(recv, nb),
			PfxSent:    pfx(sent, nb),
		}
		if v, ok := peerAS[nb]; ok {
			p.PeerAS = fmt.Sprintf("%.0f", v)
		}
		if v, ok := trans[nb]; ok {
			p.Transitions = fmt.Sprintf("%.0f", v)
		}
		peers = append(peers, p)
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].Neighbor < peers[j].Neighbor })
	return peers
}
