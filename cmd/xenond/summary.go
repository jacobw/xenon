package main

import (
	"fmt"
	"sort"

	"xenon/internal/metrics"
	"xenon/internal/probe"
)

// sensorRow is one environmental sensor (temperature) for the overview Sensors
// widget — value coloured by threshold, drillable to its history.
type sensorRow struct {
	Name  string
	Value string
	Class string // ok | warn | bad
}

// deviceSummary is the at-a-glance roll-up for the device overview: per-subsystem
// counts plus the underlying lists, so each widget renders compact, coloured and
// drillable without re-querying. No separate "attention" list — the colour is the
// signal (LibreNMS model).
type deviceSummary struct {
	PortsUp, PortsDown int
	SubsOK             int
	Subs               []component
	Sensors            []sensorRow
	Optics             []optic
	BGP                []bgpPeer
	BGPUp              int
}

func (s deviceSummary) PortsTotal() int { return s.PortsUp + s.PortsDown }

func tempClass(c float64) string {
	switch {
	case c >= 68:
		return "bad"
	case c >= 55:
		return "warn"
	default:
		return "ok"
	}
}

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

	s.Subs = buildComponents(mc, source)
	for _, c := range s.Subs {
		if c.Class != "bad" { // a disabled/standby slot is not a fault
			s.SubsOK++
		}
	}

	temps := mc.VectorBy(fmt.Sprintf(`components_component_state_temperature_instant{source=%q}`, source), "component_name")
	names := make([]string, 0, len(temps))
	for n := range temps {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		s.Sensors = append(s.Sensors, sensorRow{n, cFmt(temps[n]), tempClass(temps[n])})
	}

	s.Optics = buildOptics(mc, source)

	s.BGP = buildRouting(mc, source, meta)
	for _, p := range s.BGP {
		if p.State == "ESTABLISHED" {
			s.BGPUp++
		}
	}
	return s
}
