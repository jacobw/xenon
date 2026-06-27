package main

import (
	"fmt"
	"sort"

	"xenon/internal/alarms"
	"xenon/internal/chart"
	"xenon/internal/inventory"
	"xenon/internal/metrics"
)

type tile struct{ Label, Value string }

type toprow struct {
	DevID string
	Host  string
	Iface string
	In    string
	Out   string
	tot   float64
}

type overviewData struct {
	Title  string
	Tiles  []tile
	Graphs []graph
	Top    []toprow
	Alarms []alarms.Alarm
}

// globalMetrics are fleet-wide (no source filter) versions for the overview.
var globalMetrics = map[string]metricSpec{
	"gin":   {"Throughput in", "#a371f7", func(_, _ string) string { return `8*sum(rate(interfaces_interface_state_counters_in_octets[1m]))` }, bps},
	"gout":  {"Throughput out", "#f778ba", func(_, _ string) string { return `8*sum(rate(interfaces_interface_state_counters_out_octets[1m]))` }, bps},
	"gcpu":  {"Avg CPU used", "#5b9dff", func(_, _ string) string { return `100 - avg(system_cpus_cpu_state_idle_instant)` }, pctFmt},
	"gtemp": {"Max temperature", "#d29922", func(_, _ string) string { return `max(components_component_state_temperature_instant)` }, cFmt},
}

var globalKeys = []string{"gin", "gout", "gcpu", "gtemp"}

func buildOverview(mc *metrics.Client, inv *inventory.Store, al *alarms.Store) overviewData {
	devs := inv.List()
	reachable := mc.Reachable()
	bySrc := mc.VectorBy(`count by (source)({job="gnmic"})`, "source")
	up := 0
	for _, d := range devs {
		if reachable && bySrc[d.Device.MgmtAddress] > 0 {
			up++
		}
	}
	crit, warn := al.Counts()
	od := overviewData{Title: "Overview"}
	if a := al.Active(); len(a) > 6 {
		od.Alarms = a[:6]
	} else {
		od.Alarms = a
	}
	od.Tiles = append(od.Tiles,
		tile{"Devices", fmt.Sprintf("%d", len(devs))},
		tile{"Up", fmt.Sprintf("%d", up)},
		tile{"Alarms", fmt.Sprintf("%d", crit+warn)})
	if v, ok := mc.Scalar(`count({job="gnmic"})`); ok {
		od.Tiles = append(od.Tiles, tile{"Live series", fmt.Sprintf("%.0f", v)})
	}
	if v, ok := mc.Scalar(`8*sum(rate(interfaces_interface_state_counters_in_octets[1m])) + 8*sum(rate(interfaces_interface_state_counters_out_octets[1m]))`); ok {
		od.Tiles = append(od.Tiles, tile{"Throughput", bps(v)})
	}

	for _, k := range globalKeys {
		spec := globalMetrics[k]
		vals, ok := mc.RangeQuery(spec.promql("", ""), graphDur, graphStep)
		if !ok {
			continue
		}
		od.Graphs = append(od.Graphs, graph{Key: k, Title: spec.title, Cur: spec.format(vals[len(vals)-1]), SVG: chart.Line(vals, graphW, graphH, spec.color)})
	}

	od.Top = topInterfaces(mc, devs)
	return od
}

// topInterfaces aggregates per-interface in/out rates across the whole fleet.
func topInterfaces(mc *metrics.Client, devs []inventory.Onboarded) []toprow {
	src2dev := map[string]inventory.Onboarded{}
	for _, d := range devs {
		src2dev[d.Device.MgmtAddress] = d
	}
	type key struct{ src, iface string }
	agg := map[key]*toprow{}
	row := func(k key) *toprow {
		r := agg[k]
		if r == nil {
			r = &toprow{Iface: k.iface, Host: k.src, In: bps(0), Out: bps(0)}
			if d, ok := src2dev[k.src]; ok {
				r.DevID, r.Host = d.Device.ID, d.Device.Hostname
			}
			agg[k] = r
		}
		return r
	}
	for _, l := range mc.VectorFull(`8*rate(interfaces_interface_state_counters_in_octets[1m])`) {
		r := row(key{l.Labels["source"], l.Labels["interface_name"]})
		r.tot += l.Val
		r.In = bps(l.Val)
	}
	for _, l := range mc.VectorFull(`8*rate(interfaces_interface_state_counters_out_octets[1m])`) {
		r := row(key{l.Labels["source"], l.Labels["interface_name"]})
		r.tot += l.Val
		r.Out = bps(l.Val)
	}
	out := make([]toprow, 0, len(agg))
	for _, r := range agg {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].tot != out[j].tot {
			return out[i].tot > out[j].tot
		}
		return out[i].Host < out[j].Host
	})
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}

// buildGlobalGraphDetail renders the fleet-wide drill-down chart.
func buildGlobalGraphDetail(mc *metrics.Client, m, r string) (graphDetail, bool) {
	if r == "" {
		r = "1h"
	}
	dur, step := rangeParams(r)
	spec, ok := globalMetrics[m]
	if !ok {
		return graphDetail{}, false
	}
	vals, ok := mc.RangeQuery(spec.promql("", ""), dur, step)
	if !ok {
		return graphDetail{}, false
	}
	mn, mx, last := vals[0], vals[0], vals[len(vals)-1]
	for _, v := range vals {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
	}
	return graphDetail{
		Title:  spec.title,
		Base:   fmt.Sprintf("/graph?m=%s", m),
		Range:  r, Ranges: graphRanges,
		Cur: spec.format(last), Min: spec.format(mn), Max: spec.format(mx),
		SVG: chart.Line(vals, detailW, detailH, spec.color),
	}, true
}
