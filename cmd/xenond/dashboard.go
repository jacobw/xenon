package main

import (
	"fmt"
	"html/template"
	"net/url"
	"sort"
	"strings"
	"time"

	"xenon/internal/chart"
	"xenon/internal/metrics"
)

// errRateQuery is the combined in+out error+discard rate (per second). With iface
// "" it sums by interface for the ports table; with an iface it is scoped to that
// interface (used for the alarm-aligned drill column).
func errRateQuery(source, iface string) string {
	sel := fmt.Sprintf("source=%q", source)
	if iface != "" {
		sel = fmt.Sprintf("source=%q,interface_name=%q", source, iface)
	}
	var parts []string
	for _, l := range []string{"in_errors", "out_errors", "in_discards", "out_discards"} {
		parts = append(parts, fmt.Sprintf("rate(interfaces_interface_state_counters_%s{%s}[5m])", l, sel))
	}
	q := strings.Join(parts, " + ")
	if iface == "" {
		return "sum by (interface_name)(" + q + ")"
	}
	return q
}

// graph is one rendered panel (small sparkline). Key (+ optional Iface) drives the
// drill-down endpoint; Wide marks a full-width headline graph.
type graph struct {
	Key   string
	Iface string // drill sub-key: interface or sensor component ("" = device-wide)
	Title string
	Cur   string
	Wide  bool
	SVG   template.HTML
}

// port is one interface row for the ports table, with a mini in/out sparkline.
type port struct {
	Name        string
	Status      string // UP / DOWN (from oper-status state-set); "" if unknown
	StatusClass string // ok | bad
	Speed       string // link speed, e.g. 1G (from high-speed); "—" if unknown
	In          string
	Out         string
	Err         string
	ErrClass    string // "" when clean, "bad" when errors/discards are nonzero
	SVG         template.HTML
	tot         float64
}


// isPhysicalPort keeps real front-panel / aggregate interfaces and filters out
// Junos internal pseudo-interfaces (bme0, cbp0, dsc, esi, fti0, …).
func isPhysicalPort(name string) bool {
	for _, p := range []string{"ge-", "xe-", "et-", "mge-", "ae", "em", "fxp", "irb"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
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
func dbmFmt(v float64) string { return fmt.Sprintf("%.2f dBm", v) }
func maFmt(v float64) string  { return fmt.Sprintf("%.1f mA", v) }

// speedFmt renders an interface high-speed value (Mbps) as e.g. 1G / 10G / 100M.
func speedFmt(mbps float64) string {
	switch {
	case mbps <= 0:
		return "—"
	case mbps >= 1000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", mbps/1000), ".0") + "G"
	default:
		return fmt.Sprintf("%.0fM", mbps)
	}
}

// graphMetrics is the registry the drill-down endpoint and health panels share.
var graphMetrics = map[string]metricSpec{
	"cpu":  {"CPU used", "#5b9dff", func(s, _ string) string { return fmt.Sprintf(`100 - avg(system_cpus_cpu_state_idle_instant{source=%q})`, s) }, pctFmt},
	"mem":  {"Memory used", "#3fb950", func(s, _ string) string { return fmt.Sprintf(`system_memory_state_used{source=%q}/1073741824`, s) }, gbFmt},
	"temp": {"Temperature", "#d29922", func(s, c string) string {
		if c != "" {
			return fmt.Sprintf(`components_component_state_temperature_instant{source=%q,component_name=%q}`, s, c)
		}
		return fmt.Sprintf(`max(components_component_state_temperature_instant{source=%q})`, s)
	}, cFmt},
	"in":   {"Throughput in", "#a371f7", func(s, _ string) string { return fmt.Sprintf(`8*sum(rate(interfaces_interface_state_counters_in_octets{source=%q}[1m]))`, s) }, bps},
	"out":  {"Throughput out", "#f778ba", func(s, _ string) string { return fmt.Sprintf(`8*sum(rate(interfaces_interface_state_counters_out_octets{source=%q}[1m]))`, s) }, bps},
	// Optics (per transceiver component); iface carries the component_name.
	"optic_rx":   {"Rx power", "#3fb950", func(s, c string) string { return fmt.Sprintf(`components_component_transceiver_state_input_power_instant{source=%q,component_name=%q}`, s, c) }, dbmFmt},
	"optic_tx":   {"Tx power", "#a371f7", func(s, c string) string { return fmt.Sprintf(`components_component_transceiver_state_output_power_instant{source=%q,component_name=%q}`, s, c) }, dbmFmt},
	"optic_bias": {"Laser bias", "#d29922", func(s, c string) string { return fmt.Sprintf(`components_component_transceiver_state_laser_bias_current_instant{source=%q,component_name=%q}`, s, c) }, maFmt},
}

// trafficGraph builds the device-wide overall in/out dual sparkline (the page
// headline). Returns false when no interface counters are available.
func trafficGraph(mc *metrics.Client, source string, w, h int, wide bool) (graph, bool) {
	inV, _ := mc.RangeQuery(graphMetrics["in"].promql(source, ""), graphDur, graphStep)
	outV, _ := mc.RangeQuery(graphMetrics["out"].promql(source, ""), graphDur, graphStep)
	if len(inV) == 0 && len(outV) == 0 {
		return graph{}, false
	}
	cur := ""
	if len(inV) > 0 {
		cur = "↓ " + bps(inV[len(inV)-1])
	}
	if len(outV) > 0 {
		if cur != "" {
			cur += " · "
		}
		cur += "↑ " + bps(outV[len(outV)-1])
	}
	return graph{Key: "traffic", Title: "Overall traffic", Cur: cur, Wide: wide, SVG: chart.Dual(inV, outV, w, h, "#a371f7", "#f778ba")}, true
}

// lineGraph renders a single device-wide metric sparkline by registry key.
func lineGraph(mc *metrics.Client, source, key string) (graph, bool) {
	spec := graphMetrics[key]
	vals, ok := mc.RangeQuery(spec.promql(source, ""), graphDur, graphStep)
	if !ok {
		return graph{}, false
	}
	return graph{Key: key, Title: spec.title, Cur: spec.format(vals[len(vals)-1]), SVG: chart.Line(vals, graphW, graphH, spec.color)}, true
}

// buildGraphs is the Overview tab: an overall-traffic headline plus CPU/memory/
// temperature tiles. Every tile is drill-down clickable.
func buildGraphs(mc *metrics.Client, source string) []graph {
	var gs []graph
	if g, ok := trafficGraph(mc, source, detailW, 120, true); ok {
		gs = append(gs, g)
	}
	for _, k := range []string{"cpu", "mem", "temp"} {
		if g, ok := lineGraph(mc, source, k); ok {
			gs = append(gs, g)
		}
	}
	return gs
}

func buildPorts(mc *metrics.Client, source string) []port {
	in := mc.VectorBy(fmt.Sprintf(`8*rate(interfaces_interface_state_counters_in_octets{source=%q}[1m])`, source), "interface_name")
	out := mc.VectorBy(fmt.Sprintf(`8*rate(interfaces_interface_state_counters_out_octets{source=%q}[1m])`, source), "interface_name")
	errs := mc.VectorBy(errRateQuery(source, ""), "interface_name")
	speed := mc.VectorBy(fmt.Sprintf(`interfaces_interface_state_high_speed{source=%q}`, source), "interface_name")
	status := map[string]string{}
	for _, l := range mc.VectorFull(fmt.Sprintf(`interfaces_interface_state_oper_status{source=%q}`, source)) {
		if n := l.Labels["interface_name"]; n != "" {
			status[n] = l.Labels["oper_status"]
		}
	}
	names := map[string]bool{}
	for n := range in {
		names[n] = true
	}
	for n := range out {
		names[n] = true
	}
	for n := range status { // include physical ports even when idle/down
		if isPhysicalPort(n) {
			names[n] = true
		}
	}
	ps := make([]port, 0, len(names))
	for n := range names {
		p := port{Name: n, In: bps(in[n]), Out: bps(out[n]), Err: "0", Speed: speedFmt(speed[n]), tot: in[n] + out[n]}
		if e := errs[n]; e > 0 {
			p.Err, p.ErrClass = fmt.Sprintf("%.2f/s", e), "bad"
		}
		if s, ok := status[n]; ok {
			p.Status = s
			if p.StatusClass = "ok"; s != "UP" {
				p.StatusClass = "bad"
			}
		}
		ps = append(ps, p)
	}
	sort.Slice(ps, func(i, j int) bool {
		if ps[i].tot != ps[j].tot {
			return ps[i].tot > ps[j].tot
		}
		return ps[i].Name < ps[j].Name
	})
	if len(ps) > 16 {
		ps = ps[:16]
	}
	// Mini in/out sparkline per port (LibreNMS-style); each row drills down.
	for i := range ps {
		inV, _ := mc.RangeQuery(fmt.Sprintf(`8*rate(interfaces_interface_state_counters_in_octets{source=%q,interface_name=%q}[1m])`, source, ps[i].Name), graphDur, graphStep)
		outV, _ := mc.RangeQuery(fmt.Sprintf(`8*rate(interfaces_interface_state_counters_out_octets{source=%q,interface_name=%q}[1m])`, source, ps[i].Name), graphDur, graphStep)
		if len(inV) > 0 || len(outV) > 0 {
			ps[i].SVG = chart.Dual(inV, outV, 150, 32, "#a371f7", "#f778ba")
		}
	}
	return ps
}

// buildHealth renders the device Health tab: CPU, memory, and one graph per
// temperature-sensor component.
func buildHealth(mc *metrics.Client, source string) []graph {
	var gs []graph
	for _, k := range []string{"cpu", "mem"} {
		if g, ok := lineGraph(mc, source, k); ok {
			gs = append(gs, g)
		}
	}
	comps := mc.VectorBy(fmt.Sprintf(`components_component_state_temperature_instant{source=%q}`, source), "component_name")
	names := make([]string, 0, len(comps))
	for n := range comps {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if vals, ok := mc.RangeQuery(graphMetrics["temp"].promql(source, n), graphDur, graphStep); ok {
			gs = append(gs, graph{Key: "temp", Iface: n, Title: "Temp · " + n, Cur: cFmt(vals[len(vals)-1]), SVG: chart.Line(vals, graphW, graphH, "#d29922")})
		}
	}
	return gs
}

// optic is one transceiver's light-level reading, with the Rx-power health class.
type optic struct {
	Port     string // component_name, e.g. FPC0:PIC1:PORT0:Xcvr0
	Rx       string
	Tx       string
	Bias     string
	RxClass  string // ok | warn | bad — colours the value, also drives alarms
	HasRx    bool
	SVG      template.HTML // Rx-power sparkline (drill-down clickable)
}

// opticRxStatus classifies receive optical power (dBm) into a health class using
// SFP/SFP+ rule-of-thumb bands. TODO: per-transceiver thresholds as content
// (this is the dBm case of the LibreNMS 4-threshold sensor model).
func opticRxStatus(dbm float64) string {
	switch {
	case dbm < -19 || dbm > -1.0: // near loss-of-light, or receiver overload
		return "bad"
	case dbm < -14 || dbm > -2.5:
		return "warn"
	default:
		return "ok"
	}
}

// buildOptics renders the device Optics tab: one row per lit transceiver with Rx /
// Tx / laser-bias and an Rx-power sparkline. Rx is health-coloured by threshold.
func buildOptics(mc *metrics.Client, source string) []optic {
	rx := mc.VectorBy(fmt.Sprintf(`components_component_transceiver_state_input_power_instant{source=%q}`, source), "component_name")
	tx := mc.VectorBy(fmt.Sprintf(`components_component_transceiver_state_output_power_instant{source=%q}`, source), "component_name")
	bias := mc.VectorBy(fmt.Sprintf(`components_component_transceiver_state_laser_bias_current_instant{source=%q}`, source), "component_name")

	names := map[string]bool{}
	for n := range rx {
		names[n] = true
	}
	for n := range tx {
		names[n] = true
	}
	ordered := make([]string, 0, len(names))
	for n := range names {
		ordered = append(ordered, n)
	}
	sort.Strings(ordered)

	out := make([]optic, 0, len(ordered))
	for _, n := range ordered {
		o := optic{Port: n, Rx: "—", Tx: "—", Bias: "—"}
		if v, ok := rx[n]; ok {
			o.Rx, o.HasRx, o.RxClass = dbmFmt(v), true, opticRxStatus(v)
		}
		if v, ok := tx[n]; ok {
			o.Tx = dbmFmt(v)
		}
		if v, ok := bias[n]; ok {
			o.Bias = maFmt(v)
		}
		if vals, ok := mc.RangeQuery(graphMetrics["optic_rx"].promql(source, n), graphDur, graphStep); ok {
			o.SVG = chart.Line(vals, graphW, graphH, "#3fb950")
		}
		out = append(out, o)
	}
	return out
}

// component is one chassis-level hardware component (PSU, card, FRU) with its
// operational status, for the Health tab.
type component struct {
	Type   string
	Name   string
	Status string
	Class  string // ok | warn | bad
}

func componentStatusClass(s string) string {
	switch s {
	case "ACTIVE", "ENABLED":
		return "ok"
	case "DISABLED", "INACTIVE", "STANDBY":
		return "warn" // usually an unused/standby slot, not a fault
	default:
		return "bad"
	}
}

// buildComponents lists chassis-level components (power supplies, cards, FRUs) and
// their oper-status. Ports/transceivers are excluded — they have their own tabs.
func buildComponents(mc *metrics.Client, source string) []component {
	typ := map[string]string{}
	for _, l := range mc.VectorFull(fmt.Sprintf(`components_component_state_type{source=%q}`, source)) {
		typ[l.Labels["component_name"]] = l.Labels["type"]
	}
	seen := map[string]bool{}
	var out []component
	for _, l := range mc.VectorFull(fmt.Sprintf(`components_component_state_oper_status{source=%q}`, source)) {
		n := l.Labels["component_name"]
		t := typ[n]
		if n == "" || seen[n] || t == "PORT" || t == "TRANSCEIVER" {
			continue
		}
		seen[n] = true
		out = append(out, component{Type: t, Name: n, Status: l.Labels["oper_status"], Class: componentStatusClass(l.Labels["oper_status"])})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Name < out[j].Name
	})
	return out
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
	// Optional second chart (errors/discards under a port's traffic).
	Title2 string
	Cur2   string
	SVG2   template.HTML
}

const detailW, detailH = 840, 220

// buildGraphDetail renders the large drill-down chart for a metric key (or a
// per-interface in/out traffic graph when m=="port").
func buildGraphDetail(mc *metrics.Client, id, source, m, iface, r string) (graphDetail, bool) {
	if r == "" {
		r = "1h"
	}
	dur, step := rangeParams(r)

	// Dual in/out traffic: device-wide (traffic) or per-interface (port).
	if m == "traffic" || m == "port" {
		inq := graphMetrics["in"].promql(source, "")
		outq := graphMetrics["out"].promql(source, "")
		title, base := "Overall traffic", fmt.Sprintf("/device/%s/graph?m=traffic", id)
		if m == "port" {
			inq = fmt.Sprintf(`8*rate(interfaces_interface_state_counters_in_octets{source=%q,interface_name=%q}[1m])`, source, iface)
			outq = fmt.Sprintf(`8*rate(interfaces_interface_state_counters_out_octets{source=%q,interface_name=%q}[1m])`, source, iface)
			title, base = "Port "+iface, fmt.Sprintf("/device/%s/graph?m=port&iface=%s", id, url.QueryEscape(iface))
		}
		inV, _ := mc.RangeQuery(inq, dur, step)
		outV, _ := mc.RangeQuery(outq, dur, step)
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
		gd := graphDetail{
			Title: title, Base: base,
			Range: r, Ranges: graphRanges, Dual: true,
			Cur: "↓ " + ci + " / ↑ " + co,
			SVG: chart.Dual(inV, outV, detailW, detailH, "#a371f7", "#f778ba"),
		}
		if m == "port" { // LibreNMS-style: errors/discards under the port's traffic
			gd.Title2 = "Errors + discards"
			// Junos sample mode omits zero-valued error counters, so absence means
			// clean. Use `or` so the chart still renders if only one leaf is present.
			inErr, _ := mc.RangeQuery(fmt.Sprintf(`sum(rate(interfaces_interface_state_counters_in_errors{source=%q,interface_name=%q}[1m]) or rate(interfaces_interface_state_counters_in_discards{source=%q,interface_name=%q}[1m]))`, source, iface, source, iface), dur, step)
			outErr, _ := mc.RangeQuery(fmt.Sprintf(`sum(rate(interfaces_interface_state_counters_out_errors{source=%q,interface_name=%q}[1m]) or rate(interfaces_interface_state_counters_out_discards{source=%q,interface_name=%q}[1m]))`, source, iface, source, iface), dur, step)
			if len(inErr) > 0 || len(outErr) > 0 {
				ei, eo := "0", "0"
				if len(inErr) > 0 {
					ei = fmt.Sprintf("%.3f/s", inErr[len(inErr)-1])
				}
				if len(outErr) > 0 {
					eo = fmt.Sprintf("%.3f/s", outErr[len(outErr)-1])
				}
				gd.Cur2 = "↓ " + ei + " / ↑ " + eo
				gd.SVG2 = chart.Dual(inErr, outErr, detailW, 120, "#f0883e", "#f85149")
			} else {
				gd.Cur2 = "none recorded in this range ✓"
			}
		}
		return gd, true
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
	title := spec.title
	base := fmt.Sprintf("/device/%s/graph?m=%s", id, m)
	if iface != "" { // per-sensor (e.g. a single temperature component)
		title += " · " + iface
		base += "&iface=" + url.QueryEscape(iface)
	}
	return graphDetail{
		Title: title, Base: base,
		Range: r, Ranges: graphRanges,
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
