package main

import (
	"fmt"

	"xenon/internal/metrics"
	"xenon/internal/probe"
)

// attn is one "needs attention" item on the device overview.
type attn struct {
	Sev  string // warn | bad
	Tab  string // tab to link to
	Text string
}

// deviceSummary is the at-a-glance roll-up for the device overview: per-subsystem
// counts plus a consolidated list of what currently needs attention.
type deviceSummary struct {
	PortsUp, PortsDown int
	SubsOK, SubsTotal  int
	OpticsLit          int
	BGPUp, BGPTotal    int
	Attention          []attn
}

func (s deviceSummary) PortsTotal() int { return s.PortsUp + s.PortsDown }

func (s deviceSummary) SubsClass() string {
	if s.SubsOK < s.SubsTotal {
		return "bad"
	}
	return "ok"
}
func (s deviceSummary) BGPClass() string {
	if s.BGPTotal > 0 && s.BGPUp < s.BGPTotal {
		return "warn"
	}
	return "ok"
}

// buildDeviceSummary rolls up ports, subsystems, optics and BGP into counts + a
// prioritised attention list, reusing the per-tab builders.
func buildDeviceSummary(mc *metrics.Client, source string, meta probe.Meta) deviceSummary {
	s := deviceSummary{}

	for _, l := range mc.VectorFull(fmt.Sprintf(`interfaces_interface_state_oper_status{source=%q}`, source)) {
		if !isPhysicalPort(l.Labels["interface_name"]) {
			continue
		}
		if l.Labels["oper_status"] == "UP" {
			s.PortsUp++
		} else {
			s.PortsDown++
		}
	}

	for _, c := range buildComponents(mc, source) {
		s.SubsTotal++
		if c.Class == "ok" {
			s.SubsOK++
		} else {
			s.Attention = append(s.Attention, attn{c.Class, "health", c.Type + " " + c.Name + " — " + c.Status})
		}
	}

	for _, o := range buildOptics(mc, source) {
		if o.HasRx {
			s.OpticsLit++
		}
		if o.RxClass == "warn" || o.RxClass == "bad" {
			s.Attention = append(s.Attention, attn{o.RxClass, "optics", "Optic " + o.Port + " — Rx " + o.Rx})
		}
	}

	for _, p := range buildRouting(mc, source, meta) {
		s.BGPTotal++
		if p.State == "ESTABLISHED" {
			s.BGPUp++
		} else {
			label := p.Neighbor
			if p.Desc != "" {
				label += " (" + p.Desc + ")"
			}
			s.Attention = append(s.Attention, attn{"warn", "routing", "BGP " + label + " — " + p.State})
		}
	}

	return s
}
