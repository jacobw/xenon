// Package chart renders tiny inline SVG line charts — zero dependencies, no
// client-side JS — so the control plane can show LibreNMS-style time-series
// graphs straight from server-rendered HTML.
package chart

import (
	"fmt"
	"html/template"
	"strings"
)

// Line renders an SVG line chart (with a soft area fill) for vals, scaled to
// fit w×h. Returns embeddable SVG markup.
func Line(vals []float64, w, h int, stroke string) template.HTML {
	if len(vals) == 0 {
		return template.HTML(fmt.Sprintf(`<svg width="%d" height="%d" class="spark"></svg>`, w, h))
	}
	min, max := vals[0], vals[0]
	for _, v := range vals {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	rng := max - min
	if rng == 0 {
		rng = 1
	}
	const pad = 2.0
	plotW, plotH := float64(w)-2*pad, float64(h)-2*pad
	n := len(vals)
	x := func(i int) float64 {
		if n == 1 {
			return pad
		}
		return pad + plotW*float64(i)/float64(n-1)
	}
	y := func(v float64) float64 { return pad + plotH*(1-(v-min)/rng) }

	var pts strings.Builder
	for i, v := range vals {
		fmt.Fprintf(&pts, "%.1f,%.1f ", x(i), y(v))
	}
	line := strings.TrimSpace(pts.String())
	area := fmt.Sprintf("M%.1f,%.1f L%s L%.1f,%.1f Z", x(0), float64(h)-pad, line, x(n-1), float64(h)-pad)

	return template.HTML(fmt.Sprintf(
		`<svg width="%d" height="%d" viewBox="0 0 %d %d" preserveAspectRatio="none" class="spark">`+
			`<path d="%s" fill="%s" opacity="0.12"/>`+
			`<polyline points="%s" fill="none" stroke="%s" stroke-width="1.5" vector-effect="non-scaling-stroke"/>`+
			`</svg>`,
		w, h, w, h, area, stroke, line, stroke))
}

// Dual renders two series sharing one y-scale: a with a soft area fill, b as a
// plain line — used for interface in/out traffic graphs.
func Dual(a, b []float64, w, h int, ca, cb string) template.HTML {
	all := append(append([]float64{}, a...), b...)
	if len(all) == 0 {
		return template.HTML(fmt.Sprintf(`<svg width="%d" height="%d" class="spark"></svg>`, w, h))
	}
	min, max := all[0], all[0]
	for _, v := range all {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	rng := max - min
	if rng == 0 {
		rng = 1
	}
	const pad = 2.0
	plotW, plotH := float64(w)-2*pad, float64(h)-2*pad
	poly := func(vals []float64) string {
		var sb strings.Builder
		n := len(vals)
		for i, v := range vals {
			x := pad
			if n > 1 {
				x = pad + plotW*float64(i)/float64(n-1)
			}
			fmt.Fprintf(&sb, "%.1f,%.1f ", x, pad+plotH*(1-(v-min)/rng))
		}
		return strings.TrimSpace(sb.String())
	}
	la, lb := poly(a), poly(b)
	var area string
	if len(a) > 1 {
		area = fmt.Sprintf(`<path d="M%.1f,%.1f L%s L%.1f,%.1f Z" fill="%s" opacity="0.12"/>`, pad, float64(h)-pad, la, pad+plotW, float64(h)-pad, ca)
	}
	return template.HTML(fmt.Sprintf(
		`<svg width="%d" height="%d" viewBox="0 0 %d %d" preserveAspectRatio="none" class="spark">%s`+
			`<polyline points="%s" fill="none" stroke="%s" stroke-width="1.5" vector-effect="non-scaling-stroke"/>`+
			`<polyline points="%s" fill="none" stroke="%s" stroke-width="1.5" vector-effect="non-scaling-stroke"/></svg>`,
		w, h, w, h, area, la, ca, lb, cb))
}

// palette cycles distinct line colors for multi-series charts.
var palette = []string{"#5b9dff", "#3fb950", "#d29922", "#a371f7", "#f778ba", "#56d4dd", "#e3b341", "#f85149", "#7ee787", "#ff9bce", "#79c0ff", "#ffa657"}

// Color returns a stable palette color for index i.
func Color(i int) string { return palette[i%len(palette)] }

// MultiSeries is one labelled line for a Multi chart.
type MultiSeries struct {
	Label string
	Color string
	Vals  []float64
}

// Multi renders N series sharing one y-scale (no fill) — for Explore's
// group-by/aggregate graphs.
func Multi(ss []MultiSeries, w, h int) template.HTML {
	have := false
	var min, max float64
	for _, s := range ss {
		for _, v := range s.Vals {
			if !have {
				min, max, have = v, v, true
				continue
			}
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
		}
	}
	if !have {
		return template.HTML(fmt.Sprintf(`<svg width="%d" height="%d" class="spark"></svg>`, w, h))
	}
	rng := max - min
	if rng == 0 {
		rng = 1
	}
	const pad = 2.0
	plotW, plotH := float64(w)-2*pad, float64(h)-2*pad
	var b strings.Builder
	fmt.Fprintf(&b, `<svg width="%d" height="%d" viewBox="0 0 %d %d" preserveAspectRatio="none" class="spark">`, w, h, w, h)
	for _, s := range ss {
		n := len(s.Vals)
		var pts strings.Builder
		for i, v := range s.Vals {
			x := pad
			if n > 1 {
				x = pad + plotW*float64(i)/float64(n-1)
			}
			fmt.Fprintf(&pts, "%.1f,%.1f ", x, pad+plotH*(1-(v-min)/rng))
		}
		fmt.Fprintf(&b, `<polyline points="%s" fill="none" stroke="%s" stroke-width="1.5" vector-effect="non-scaling-stroke"/>`, strings.TrimSpace(pts.String()), s.Color)
	}
	b.WriteString("</svg>")
	return template.HTML(b.String())
}
