package main

import (
	"fmt"
	"net/url"
	"sort"

	"xenon/internal/metrics"
	"xenon/internal/probe"
)

// metricOption is one entry in the interface drill-down's metric selector.
type metricOption struct {
	Key   string
	Label string
	On    bool
}

// graphView is the drill-down shell rendered server-side; the chart itself is drawn
// client-side (uPlot) from DataURL, so it's zoomable with hover tooltips.
type graphView struct {
	ID       string
	Title    string
	Metric   string
	Iface    string
	Range    string
	Unit     string
	Ranges   []string
	Selector []metricOption // interface metric chooser (throughput/packets/errors/…)
	DataURL  string
}

// descFor returns the description for an interface or BGP-neighbour key.
func descFor(group, key string, meta probe.Meta) string {
	if group == "bgp" {
		return meta.BGP[key]
	}
	return meta.Interfaces[key]
}

// buildGraphView assembles the drill-down shell for a metric (normalising the
// legacy m=port to throughput) and, for interface graphs, the metric selector.
func buildGraphView(id, m, iface, r string, meta probe.Meta) (graphView, bool) {
	if m == "port" {
		m = "throughput"
	}
	if r == "" {
		r = "1h"
	}
	spec, ok := graphSpecs[m]
	if !ok {
		return graphView{}, false
	}
	title := spec.Label
	if iface != "" {
		entity := iface
		if desc := descFor(spec.Group, iface, meta); desc != "" {
			entity = iface + " (" + desc + ")"
		}
		if spec.Group != "" { // entity-first: "ge-0/0/0 (uplink) · Throughput"
			title = entity + " · " + spec.Label
		} else { // metric-first: "Rx power · Xcvr0", "Temperature · sensor"
			title = spec.Label + " · " + entity
		}
	}
	gv := graphView{ID: id, Title: title, Metric: m, Iface: iface, Range: r, Unit: spec.Unit, Ranges: graphRanges}
	for _, k := range selectorOrder[spec.Group] {
		gv.Selector = append(gv.Selector, metricOption{Key: k, Label: graphSpecs[k].Label, On: k == m})
	}
	gv.DataURL = fmt.Sprintf("/device/%s/series?m=%s&iface=%s&r=%s", id, m, url.QueryEscape(iface), url.QueryEscape(r))
	return gv, true
}

// seriesSpec is one line in a graph; q builds its PromQL from (source, iface).
type seriesSpec struct {
	Name  string
	Color string
	q     func(source, iface string) string
}

// graphSpec is a graph "template": a titled, unit-typed set of series. Iface specs
// need an interface_name; Group ties the selectable per-interface templates together.
type graphSpec struct {
	Label  string
	Unit   string
	Iface  bool
	Group  string
	Series []seriesSpec
}

func ifBits(leaf string) func(string, string) string {
	return func(s, i string) string {
		return fmt.Sprintf(`8*rate(interfaces_interface_state_counters_%s{source=%q,interface_name=%q}[2m])`, leaf, s, i)
	}
}
func ifRate(leaf string) func(string, string) string {
	return func(s, i string) string {
		return fmt.Sprintf(`rate(interfaces_interface_state_counters_%s{source=%q,interface_name=%q}[2m])`, leaf, s, i)
	}
}
func ifErrSum(dir string) func(string, string) string {
	return func(s, i string) string {
		return fmt.Sprintf(`rate(interfaces_interface_state_counters_%s_errors{source=%q,interface_name=%q}[2m]) + rate(interfaces_interface_state_counters_%s_discards{source=%q,interface_name=%q}[2m])`, dir, s, i, dir, s, i)
	}
}

// ifaceGraphOrder is the metric selector for the interface drill-down, in order.
var ifaceGraphOrder = []string{"throughput", "packets", "errors", "unicast"}

// bgpGraphOrder is the metric selector for the BGP-neighbour drill-down.
var bgpGraphOrder = []string{"bgp_prefixes", "bgp_accepted", "bgp_installed", "bgp_flaps"}

// selectorOrder maps a graph group to its in-order selector keys.
var selectorOrder = map[string][]string{"iface": ifaceGraphOrder, "bgp": bgpGraphOrder}

// bgpPfx queries an afi-safi prefix count (summed over afi-safis) for a neighbour;
// bgpState queries a neighbour/state counter. iface carries neighbor_neighbor_address.
func bgpPfx(leaf string) func(string, string) string {
	return func(s, nb string) string {
		return fmt.Sprintf(`sum(network_instances_network_instance_protocols_protocol_bgp_neighbors_neighbor_afi_safis_afi_safi_state_prefixes_%s{source=%q,neighbor_neighbor_address=%q})`, leaf, s, nb)
	}
}
func bgpState(leaf string) func(string, string) string {
	return func(s, nb string) string {
		return fmt.Sprintf(`sum(network_instances_network_instance_protocols_protocol_bgp_neighbors_neighbor_state_%s{source=%q,neighbor_neighbor_address=%q})`, leaf, s, nb)
	}
}

// graphSpecs is the drill-down registry, shared by the series endpoint. Device
// graphs are device-wide; iface graphs need an interface and form a selectable set.
var graphSpecs = map[string]graphSpec{
	// device-wide
	"traffic": {"Overall traffic", "bps", false, "", []seriesSpec{
		{"In", "#a371f7", func(s, _ string) string { return fmt.Sprintf(`8*sum(rate(interfaces_interface_state_counters_in_octets{source=%q}[2m]))`, s) }},
		{"Out", "#f778ba", func(s, _ string) string { return fmt.Sprintf(`8*sum(rate(interfaces_interface_state_counters_out_octets{source=%q}[2m]))`, s) }},
	}},
	"cpu": {"CPU used", "%", false, "", []seriesSpec{
		{"CPU", "#5b9dff", func(s, _ string) string { return fmt.Sprintf(`100 - avg(system_cpus_cpu_state_idle_instant{source=%q})`, s) }},
	}},
	"mem": {"Memory used", "GB", false, "", []seriesSpec{
		{"Memory", "#3fb950", func(s, _ string) string { return fmt.Sprintf(`system_memory_state_used{source=%q}/1073741824`, s) }},
	}},
	"temp": {"Temperature", "°C", false, "", []seriesSpec{
		{"Temp", "#d29922", func(s, c string) string {
			if c != "" {
				return fmt.Sprintf(`components_component_state_temperature_instant{source=%q,component_name=%q}`, s, c)
			}
			return fmt.Sprintf(`max(components_component_state_temperature_instant{source=%q})`, s)
		}},
	}},
	// optics (per transceiver component, iface carries component_name)
	"optic_rx":   {"Rx power", "dBm", true, "", []seriesSpec{{"Rx", "#3fb950", func(s, c string) string { return fmt.Sprintf(`components_component_transceiver_state_input_power_instant{source=%q,component_name=%q}`, s, c) }}}},
	"optic_tx":   {"Tx power", "dBm", true, "", []seriesSpec{{"Tx", "#a371f7", func(s, c string) string { return fmt.Sprintf(`components_component_transceiver_state_output_power_instant{source=%q,component_name=%q}`, s, c) }}}},
	"optic_bias": {"Laser bias", "mA", true, "", []seriesSpec{{"Bias", "#d29922", func(s, c string) string { return fmt.Sprintf(`components_component_transceiver_state_laser_bias_current_instant{source=%q,component_name=%q}`, s, c) }}}},

	// component operational-state history (numeric encode: 2=active, 1=disabled,
	// 0=fault) — shows WHEN a subsystem's state changed.
	"comp_state": {"Operational state", "state", true, "", []seriesSpec{{"State", "#5b9dff", func(s, c string) string {
		return fmt.Sprintf(`2*components_component_state_oper_status{source=%q,component_name=%q,oper_status="ACTIVE"} or 1*components_component_state_oper_status{source=%q,component_name=%q,oper_status="DISABLED"} or 0*components_component_state_oper_status{source=%q,component_name=%q}`, s, c, s, c, s, c)
	}}}},

	// per-interface, selectable group
	"throughput": {"Throughput", "bps", true, "iface", []seriesSpec{
		{"In", "#a371f7", ifBits("in_octets")}, {"Out", "#f778ba", ifBits("out_octets")},
	}},
	"packets": {"Packets", "pps", true, "iface", []seriesSpec{
		{"In", "#a371f7", ifRate("in_pkts")}, {"Out", "#f778ba", ifRate("out_pkts")},
	}},
	"errors": {"Errors + discards", "/s", true, "iface", []seriesSpec{
		{"In", "#f0883e", ifErrSum("in")}, {"Out", "#f85149", ifErrSum("out")},
	}},
	"unicast": {"Unicast packets", "pps", true, "iface", []seriesSpec{
		{"In", "#a371f7", ifRate("in_unicast_pkts")}, {"Out", "#f778ba", ifRate("out_unicast_pkts")},
	}},

	// per-BGP-neighbour, selectable group (iface carries neighbor_neighbor_address)
	"bgp_prefixes": {"Prefixes", "count", true, "bgp", []seriesSpec{
		{"Received", "#3fb950", bgpPfx("received")}, {"Advertised", "#a371f7", bgpPfx("sent")},
	}},
	"bgp_accepted": {"Accepted", "count", true, "bgp", []seriesSpec{
		{"Received", "#a371f7", bgpPfx("received")}, {"Accepted", "#3fb950", bgpPfx("accepted")},
	}},
	"bgp_installed": {"Installed", "count", true, "bgp", []seriesSpec{
		{"Installed", "#5b9dff", bgpPfx("installed")},
	}},
	"bgp_flaps": {"Flaps", "count", true, "bgp", []seriesSpec{
		{"Transitions", "#d29922", bgpState("established_transitions")},
	}},
}

// seriesData is the uPlot-shaped JSON the client charts render.
type seriesData struct {
	Title  string       `json:"title"`
	Unit   string       `json:"unit"`
	Series []string     `json:"series"`
	Colors []string     `json:"colors"`
	Times  []int64      `json:"times"`  // unix seconds (uPlot x-axis)
	Values [][]*float64 `json:"values"` // per series, aligned to Times; null = gap
}

// buildSeriesData runs each series' range query and aligns them onto a shared time
// axis (null-filling gaps) for client-side rendering.
func buildSeriesData(mc *metrics.Client, source, m, iface, r string) (seriesData, bool) {
	spec, ok := graphSpecs[m]
	if !ok {
		return seriesData{}, false
	}
	dur, step := rangeParams(r)

	maps := make([]map[int64]float64, len(spec.Series))
	tset := map[int64]bool{}
	for i, ss := range spec.Series {
		ts, vals := mc.RangePoints(ss.q(source, iface), dur, step)
		mp := make(map[int64]float64, len(ts))
		for j, t := range ts {
			mp[t] = vals[j]
			tset[t] = true
		}
		maps[i] = mp
	}
	if len(tset) == 0 {
		return seriesData{}, false
	}
	times := make([]int64, 0, len(tset))
	for t := range tset {
		times = append(times, t)
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })

	out := seriesData{Title: spec.Label, Unit: spec.Unit, Times: times}
	for i, ss := range spec.Series {
		out.Series = append(out.Series, ss.Name)
		out.Colors = append(out.Colors, ss.Color)
		col := make([]*float64, len(times))
		for j, t := range times {
			if v, ok := maps[i][t]; ok {
				vv := v
				col[j] = &vv
			}
		}
		out.Values = append(out.Values, col)
	}
	return out, true
}
