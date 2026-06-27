package main

import (
	"fmt"
	"html/template"
	"net/url"
	"sort"
	"time"

	"xenon/internal/chart"
	"xenon/internal/metrics"
)

// graph is one rendered health panel (small sparkline). Key drives drill-down.
type graph struct {
	Key   string
	Title string
	Cur   string
	SVG   template.HTML
}

// port is one interface row for the ports table.
type port struct {
	Name string
	In   string
	Out  string
	tot  float64
}

const (
	graphDur  = 15 * time.Minute
	graphStep = 15 * time.Second
	graphW    = 300
	graphH    = 64
)

type metricSpec struct {
	title, color string
	promql       func(source, iface string) string
	format       func(float64) string
}

func pctFmt(v float64) string { return fmt.Sprintf("%.0f%%", v) }
func gbFmt(v float64) string  { return fmt.Sprintf("%.2f GB", v) }
func cFmt(v float64) string   { return fmt.Sprintf("%.0f °C", v) }

// graphMetrics is the registry the drill-down endpoint and health panels share.
var graphMetrics = map[string]metricSpec{
	"cpu":  {"CPU used", "#5b9dff", func(s, _ string) string { return fmt.Sprintf(`100 - avg(system_cpus_cpu_state_idle_instant{source=%q})`, s) }, pctFmt},
	"mem":  {"Memory used", "#3fb950", func(s, _ string) string { return fmt.Sprintf(`system_memory_state_used{source=%q}/1073741824`, s) }, gbFmt},
	"temp": {"Temperature", "#d29922", func(s, _ string) string { return fmt.Sprintf(`max(components_component_state_temperature_instant{source=%q})`, s) }, cFmt},
	"in":   {"Throughput in", "#a371f7", func(s, _ string) string { return fmt.Sprintf(`8*sum(rate(interfaces_interface_state_counters_in_octets{source=%q}[1m]))`, s) }, bps},
	"out":  {"Throughput out", "#f778ba", func(s, _ string) string { return fmt.Sprintf(`8*sum(rate(interfaces_interface_state_counters_out_octets{source=%q}[1m]))`, s) }, bps},
}

// healthKeys are the four sparkline panels on the device page, in order.
var healthKeys = []string{"cpu", "mem", "temp", "in"}

func buildGraphs(mc *metrics.Client, source string) []graph {
	var gs []graph
	for _, k := range healthKeys {
		spec := graphMetrics[k]
		vals, ok := mc.RangeQuery(spec.promql(source, ""), graphDur, graphStep)
		if !ok {
			continue
		}
		gs = append(gs, graph{Key: k, Title: spec.title, Cur: spec.format(vals[len(vals)-1]), SVG: chart.Line(vals, graphW, graphH, spec.color)})
	}
	return gs
}

func buildPorts(mc *metrics.Client, source string) []port {
	in := mc.VectorBy(fmt.Sprintf(`8*rate(interfaces_interface_state_counters_in_octets{source=%q}[1m])`, source), "interface_name")
	out := mc.VectorBy(fmt.Sprintf(`8*rate(interfaces_interface_state_counters_out_octets{source=%q}[1m])`, source), "interface_name")
	names := map[string]bool{}
	for n := range in {
		names[n] = true
	}
	for n := range out {
		names[n] = true
	}
	ps := make([]port, 0, len(names))
	for n := range names {
		ps = append(ps, port{Name: n, In: bps(in[n]), Out: bps(out[n]), tot: in[n] + out[n]})
	}
	sort.Slice(ps, func(i, j int) bool {
		if ps[i].tot != ps[j].tot {
			return ps[i].tot > ps[j].tot
		}
		return ps[i].Name < ps[j].Name
	})
	if len(ps) > 12 {
		ps = ps[:12]
	}
	return ps
}

// buildHealth renders the device Health tab: CPU, memory, and one graph per
// temperature-sensor component.
func buildHealth(mc *metrics.Client, source string) []graph {
	var gs []graph
	for _, k := range []string{"cpu", "mem"} {
		spec := graphMetrics[k]
		if vals, ok := mc.RangeQuery(spec.promql(source, ""), graphDur, graphStep); ok {
			gs = append(gs, graph{Key: k, Title: spec.title, Cur: spec.format(vals[len(vals)-1]), SVG: chart.Line(vals, graphW, graphH, spec.color)})
		}
	}
	comps := mc.VectorBy(fmt.Sprintf(`components_component_state_temperature_instant{source=%q}`, source), "component_name")
	names := make([]string, 0, len(comps))
	for n := range comps {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if vals, ok := mc.RangeQuery(fmt.Sprintf(`components_component_state_temperature_instant{source=%q,component_name=%q}`, source, n), graphDur, graphStep); ok {
			gs = append(gs, graph{Key: "temp", Title: "Temp · " + n, Cur: cFmt(vals[len(vals)-1]), SVG: chart.Line(vals, graphW, graphH, "#d29922")})
		}
	}
	return gs
}

// ---- drill-down detail ----

var graphRanges = []string{"1h", "6h", "24h", "7d"}

func rangeParams(r string) (time.Duration, time.Duration) {
	switch r {
	case "6h":
		return 6 * time.Hour, 2 * time.Minute
	case "24h":
		return 24 * time.Hour, 10 * time.Minute
	case "7d":
		return 7 * 24 * time.Hour, time.Hour
	default:
		return time.Hour, 15 * time.Second
	}
}

type graphDetail struct {
	Title  string
	Base   string // hx-get base (range appended)
	Range  string
	Ranges []string
	Cur    string
	Min    string
	Max    string
	Dual   bool
	SVG    template.HTML
}

const detailW, detailH = 840, 220

// buildGraphDetail renders the large drill-down chart for a metric key (or a
// per-interface in/out traffic graph when m=="port").
func buildGraphDetail(mc *metrics.Client, id, source, m, iface, r string) (graphDetail, bool) {
	if r == "" {
		r = "1h"
	}
	dur, step := rangeParams(r)

	if m == "port" {
		inV, _ := mc.RangeQuery(fmt.Sprintf(`8*rate(interfaces_interface_state_counters_in_octets{source=%q,interface_name=%q}[1m])`, source, iface), dur, step)
		outV, _ := mc.RangeQuery(fmt.Sprintf(`8*rate(interfaces_interface_state_counters_out_octets{source=%q,interface_name=%q}[1m])`, source, iface), dur, step)
		if len(inV) == 0 && len(outV) == 0 {
			return graphDetail{}, false
		}
		ci, co := "—", "—"
		if len(inV) > 0 {
			ci = bps(inV[len(inV)-1])
		}
		if len(outV) > 0 {
			co = bps(outV[len(outV)-1])
		}
		return graphDetail{
			Title:  "Port " + iface,
			Base:   fmt.Sprintf("/device/%s/graph?m=port&iface=%s", id, url.QueryEscape(iface)),
			Range:  r, Ranges: graphRanges, Dual: true,
			Cur: "↓ " + ci + " / ↑ " + co,
			SVG: chart.Dual(inV, outV, detailW, detailH, "#a371f7", "#f778ba"),
		}, true
	}

	spec, ok := graphMetrics[m]
	if !ok {
		return graphDetail{}, false
	}
	vals, ok := mc.RangeQuery(spec.promql(source, iface), dur, step)
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
		Base:   fmt.Sprintf("/device/%s/graph?m=%s", id, m),
		Range:  r, Ranges: graphRanges,
		Cur: spec.format(last), Min: spec.format(mn), Max: spec.format(mx),
		SVG: chart.Line(vals, detailW, detailH, spec.color),
	}, true
}

// bps formats a bits-per-second value with decimal (1000) units.
func bps(v float64) string {
	u := []string{"bps", "Kbps", "Mbps", "Gbps", "Tbps"}
	i := 0
	for v >= 1000 && i < len(u)-1 {
		v /= 1000
		i++
	}
	return fmt.Sprintf("%.1f %s", v, u[i])
}
