package main

import (
	"fmt"
	"sort"

	"xenon/internal/metrics"
)

// bgpPeer is one BGP neighbour row for the Routing tab.
type bgpPeer struct {
	Neighbor    string
	Network     string // network-instance (VRF) name
	PeerAS      string
	State       string // ESTABLISHED / ACTIVE / IDLE / …
	StateClass  string // ok | warn | bad
	Transitions string // established-transitions (flap count)
}

// bgpBase is the (long) metric-name prefix for OpenConfig BGP neighbour state.
const bgpBase = "network_instances_network_instance_protocols_protocol_bgp_neighbors_neighbor_state_"

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
func buildRouting(mc *metrics.Client, source string) []bgpPeer {
	peerAS := mc.VectorBy(fmt.Sprintf(bgpBase+`peer_as{source=%q}`, source), "neighbor_neighbor_address")
	trans := mc.VectorBy(fmt.Sprintf(bgpBase+`established_transitions{source=%q}`, source), "neighbor_neighbor_address")

	var peers []bgpPeer
	for _, l := range mc.VectorFull(fmt.Sprintf(bgpBase+`session_state{source=%q}`, source)) {
		nb := l.Labels["neighbor_neighbor_address"]
		if nb == "" {
			continue
		}
		p := bgpPeer{
			Neighbor:   nb,
			Network:    l.Labels["network_instance_name"],
			State:      l.Labels["session_state"],
			StateClass: bgpStateClass(l.Labels["session_state"]),
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
