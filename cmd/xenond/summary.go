package main

import (
	"fmt"

	"xenon/internal/alarms"
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
// counts, the underlying lists (so widgets render compactly without re-querying),
// and a consolidated list of what currently needs attention.
type deviceSummary struct {
	PortsUp, PortsDown int
	SubsOK             int
	Subs               []component
	Optics             []optic
	BGP                []bgpPeer
	BGPUp              int
	Attention          []attn
}

func (s deviceSummary) PortsTotal() int { return s.PortsUp + s.PortsDown }

// buildDeviceSummary rolls up alarms, ports, subsystems, optics and BGP into the
// overview's counts, lists and prioritised attention feed.
func buildDeviceSummary(mc *metrics.Client, source string, meta probe.Meta, active []alarms.Alarm) deviceSummary {
	s := deviceSummary{}

	for _, a := range active { // alarms first — highest priority
		sev := "warn"
		if a.Severity == "critical" {
			sev = "bad"
		}
		s.Attention = append(s.Attention, attn{sev, "alarms", a.Summary})
	}

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

	s.Subs = buildComponents(mc, source)
	for _, c := range s.Subs {
		if c.Class == "ok" {
			s.SubsOK++
		} else {
			s.Attention = append(s.Attention, attn{c.Class, "health", c.Type + " " + c.Name + " — " + c.Status})
		}
	}

	s.Optics = buildOptics(mc, source)
	for _, o := range s.Optics {
		if o.RxClass == "warn" || o.RxClass == "bad" {
			s.Attention = append(s.Attention, attn{o.RxClass, "optics", "Optic " + o.Port + " — Rx " + o.Rx})
		}
	}

	s.BGP = buildRouting(mc, source, meta)
	for _, p := range s.BGP {
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
