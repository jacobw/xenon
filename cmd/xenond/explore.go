package main

import (
	"fmt"
	"html/template"
	"sort"

	"xenon/internal/chart"
	"xenon/internal/metrics"
)

type opt struct{ Key, Label string }

var exploreMetricList = []opt{
	{"tin", "Throughput in"},
	{"tout", "Throughput out"},
	{"cpu", "CPU used"},
	{"mem", "Memory used"},
	{"temp", "Temperature"},
}

type exploreMetric struct {
	title  string
	promql func(label string) string
	format func(float64) string
}

// exploreMetrics aggregate a metric grouped by an arbitrary label — the core of
// the multi-device view. Aggregation matches the metric (sum traffic, avg %,
// max temp).
var exploreMetrics = map[string]exploreMetric{
	"tin":  {"Throughput in", func(l string) string { return fmt.Sprintf("sum by (%s)(8*rate(interfaces_interface_state_counters_in_octets[1m]))", l) }, bps},
	"tout": {"Throughput out", func(l string) string { return fmt.Sprintf("sum by (%s)(8*rate(interfaces_interface_state_counters_out_octets[1m]))", l) }, bps},
	"cpu":  {"CPU used", func(l string) string { return fmt.Sprintf("avg by (%s)(100 - system_cpus_cpu_state_idle_instant)", l) }, pctFmt},
	"mem":  {"Memory used", func(l string) string { return fmt.Sprintf("sum by (%s)(system_memory_state_used)/1073741824", l) }, gbFmt},
	"temp": {"Temperature", func(l string) string { return fmt.Sprintf("max by (%s)(components_component_state_temperature_instant)", l) }, cFmt},
}

var exploreGroupBys = []opt{
	{"interface_name", "interface"},
	{"device", "device"},
	{"platform", "platform"},
	{"site", "site"},
	{"role", "role"},
}

func exploreDefaults(m, by, r string) (string, string, string) {
	if _, ok := exploreMetrics[m]; !ok {
		m = "tin"
	}
	okBy := false
	for _, g := range exploreGroupBys {
		if g.Key == by {
			okBy = true
		}
	}
	if !okBy {
		by = "interface_name"
	}
	switch r {
	case "1h", "6h", "24h", "7d":
	default:
		r = "1h"
	}
	return m, by, r
}

type exploreData struct {
	Title    string
	Metrics  []opt
	GroupBys []opt
	Ranges   []string
	M, By, R string
}

func buildExplorePage(m, by, r string) exploreData {
	m, by, r = exploreDefaults(m, by, r)
	return exploreData{Title: "Explore", Metrics: exploreMetricList, GroupBys: exploreGroupBys, Ranges: graphRanges, M: m, By: by, R: r}
}

type exploreSeriesRow struct {
	Label string
	Color string
	Cur   string
}

type exploreGraphData struct {
	Title  string
	By, R  string
	SVG    template.HTML
	Series []exploreSeriesRow
	Count  int
}

const exploreW, exploreH = 860, 240

func buildExploreGraph(mc *metrics.Client, m, by, r string) exploreGraphData {
	m, by, r = exploreDefaults(m, by, r)
	spec := exploreMetrics[m]
	dur, step := rangeParams(r)
	series := mc.RangeSeries(spec.promql(by), by, dur, step)
	sort.Slice(series, func(i, j int) bool {
		li, lj := last(series[i].Vals), last(series[j].Vals)
		if li != lj {
			return li > lj
		}
		return series[i].Label < series[j].Label
	})
	var ms []chart.MultiSeries
	var rows []exploreSeriesRow
	for i, s := range series {
		col := chart.Color(i)
		ms = append(ms, chart.MultiSeries{Label: s.Label, Color: col, Vals: s.Vals})
		rows = append(rows, exploreSeriesRow{Label: s.Label, Color: col, Cur: spec.format(last(s.Vals))})
	}
	return exploreGraphData{Title: spec.title, By: by, R: r, SVG: chart.Multi(ms, exploreW, exploreH), Series: rows, Count: len(rows)}
}

func last(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	return v[len(v)-1]
}
