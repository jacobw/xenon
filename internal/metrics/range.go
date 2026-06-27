package metrics

import (
	"encoding/json"
	"net/url"
	"strconv"
	"time"
)

// RangeQuery runs a query_range over the trailing dur at the given step and
// returns the first series' values (oldest→newest).
func (c *Client) RangeQuery(promql string, dur, step time.Duration) ([]float64, bool) {
	if !c.Enabled() {
		return nil, false
	}
	now := time.Now()
	q := url.Values{}
	q.Set("query", promql)
	q.Set("start", strconv.FormatInt(now.Add(-dur).Unix(), 10))
	q.Set("end", strconv.FormatInt(now.Unix(), 10))
	q.Set("step", strconv.Itoa(int(step.Seconds())))

	resp, err := c.hc.Get(c.base + "/api/v1/query_range?" + q.Encode())
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()

	var mr struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Values [][2]any `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&mr) != nil || mr.Status != "success" || len(mr.Data.Result) == 0 {
		return nil, false
	}
	vals := make([]float64, 0, len(mr.Data.Result[0].Values))
	for _, p := range mr.Data.Result[0].Values {
		if s, ok := p[1].(string); ok {
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				vals = append(vals, f)
			}
		}
	}
	return vals, len(vals) > 0
}

// VectorBy runs an instant query and returns a map of the given label's value →
// sample value (e.g. interface_name → rate). Used for the ports table.
func (c *Client) VectorBy(promql, label string) map[string]float64 {
	out := map[string]float64{}
	if !c.Enabled() {
		return out
	}
	resp, err := c.hc.Get(c.base + "/api/v1/query?query=" + url.QueryEscape(promql))
	if err != nil {
		return out
	}
	defer resp.Body.Close()

	var vr struct {
		Data struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Value  [2]any            `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&vr) != nil {
		return out
	}
	for _, r := range vr.Data.Result {
		if s, ok := r.Value[1].(string); ok {
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				out[r.Metric[label]] = f
			}
		}
	}
	return out
}

// Labeled is a vector sample with its full label set.
type Labeled struct {
	Labels map[string]string
	Val    float64
}

// VectorFull runs an instant query and returns every sample with its labels.
func (c *Client) VectorFull(promql string) []Labeled {
	var out []Labeled
	if !c.Enabled() {
		return out
	}
	resp, err := c.hc.Get(c.base + "/api/v1/query?query=" + url.QueryEscape(promql))
	if err != nil {
		return out
	}
	defer resp.Body.Close()
	var vr struct {
		Data struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Value  [2]any            `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&vr) != nil {
		return out
	}
	for _, r := range vr.Data.Result {
		if s, ok := r.Value[1].(string); ok {
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				out = append(out, Labeled{Labels: r.Metric, Val: f})
			}
		}
	}
	return out
}

// LabeledSeries is one time-series with the value of a chosen group-by label.
type LabeledSeries struct {
	Label string
	Vals  []float64
}

// RangeSeries runs a query_range and returns every result series, each tagged
// with the value of the given label (for group-by / multi-series graphs).
func (c *Client) RangeSeries(promql, label string, dur, step time.Duration) []LabeledSeries {
	var out []LabeledSeries
	if !c.Enabled() {
		return out
	}
	now := time.Now()
	q := url.Values{}
	q.Set("query", promql)
	q.Set("start", strconv.FormatInt(now.Add(-dur).Unix(), 10))
	q.Set("end", strconv.FormatInt(now.Unix(), 10))
	q.Set("step", strconv.Itoa(int(step.Seconds())))

	resp, err := c.hc.Get(c.base + "/api/v1/query_range?" + q.Encode())
	if err != nil {
		return out
	}
	defer resp.Body.Close()
	var mr struct {
		Data struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Values [][2]any          `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&mr) != nil {
		return out
	}
	for _, r := range mr.Data.Result {
		vals := make([]float64, 0, len(r.Values))
		for _, p := range r.Values {
			if s, ok := p[1].(string); ok {
				if f, e := strconv.ParseFloat(s, 64); e == nil {
					vals = append(vals, f)
				}
			}
		}
		lbl := r.Metric[label]
		if lbl == "" {
			lbl = "—"
		}
		out = append(out, LabeledSeries{Label: lbl, Vals: vals})
	}
	return out
}
