// Package metrics is the read-plane adapter: a tiny Prometheus HTTP API client
// the control plane (M4) uses to show live series/values alongside the engine's
// planning estimates. Optional — disabled (graceful) when no URL is configured.
package metrics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Client queries a Prometheus instant API. A zero/empty base means disabled.
type Client struct {
	base string
	hc   *http.Client
}

// New returns a client for the given Prometheus base URL (e.g.
// http://localhost:9090). An empty base yields a disabled client.
func New(base string) *Client {
	if base == "" {
		return &Client{}
	}
	return &Client{base: base, hc: &http.Client{Timeout: 3 * time.Second}}
}

// Enabled reports whether a Prometheus URL is configured.
func (c *Client) Enabled() bool { return c.base != "" }

// Scalar runs an instant query and returns the first sample's numeric value.
func (c *Client) Scalar(promql string) (float64, bool) { return c.scalar(promql) }

// Reachable reports whether Prometheus answered a trivial probe query.
func (c *Client) Reachable() bool {
	if !c.Enabled() {
		return false
	}
	_, ok := c.scalar("vector(1)")
	return ok
}

// Sample is one labelled value for display.
type Sample struct{ Label, Value string }

// DeviceMetrics is the live read-plane view for one device.
type DeviceMetrics struct {
	Configured bool // a Prometheus URL is set
	Reachable  bool // Prometheus answered
	Series     int  // live series count for this device's gnmic source
	Samples    []Sample
}

// ForDevice gathers the live series count and a few highlighted values for a
// device, keyed by its gnmic `source` label (= the device mgmt address).
func (c *Client) ForDevice(source string) DeviceMetrics {
	dm := DeviceMetrics{Configured: c.Enabled()}
	if !c.Enabled() {
		return dm
	}
	if _, ok := c.scalar("vector(1)"); !ok { // reachability probe
		return dm
	}
	dm.Reachable = true

	if n, ok := c.scalar(fmt.Sprintf("count({source=%q})", source)); ok {
		dm.Series = int(n)
	}
	add := func(label, promql, format string, args ...any) {
		if v, ok := c.scalar(fmt.Sprintf(promql, args...)); ok {
			dm.Samples = append(dm.Samples, Sample{label, fmt.Sprintf(format, v)})
		}
	}
	if idle, ok := c.scalar(fmt.Sprintf("avg(system_cpus_cpu_state_idle_instant{source=%q})", source)); ok {
		dm.Samples = append(dm.Samples, Sample{"CPU used", fmt.Sprintf("%.0f%%", 100-idle)})
	}
	if used, ok := c.scalar(fmt.Sprintf("system_memory_state_used{source=%q}", source)); ok {
		if phys, ok := c.scalar(fmt.Sprintf("system_memory_state_physical{source=%q}", source)); ok && phys > 0 {
			dm.Samples = append(dm.Samples, Sample{"Memory used", fmt.Sprintf("%.0f%%", 100*used/phys)})
		}
	}
	add("Max temp", "max(components_component_state_temperature_instant{source=%q})", "%.0f °C", source)
	if v, ok := c.scalar(fmt.Sprintf("sum(interfaces_interface_state_counters_in_octets{source=%q})", source)); ok {
		dm.Samples = append(dm.Samples, Sample{"Σ in-octets", humanBytes(v)})
	}
	return dm
}

type vectorResp struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Value [2]any `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

// scalar runs an instant query and returns the first sample's numeric value.
func (c *Client) scalar(promql string) (float64, bool) {
	resp, err := c.hc.Get(c.base + "/api/v1/query?query=" + url.QueryEscape(promql))
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	var vr vectorResp
	if json.NewDecoder(resp.Body).Decode(&vr) != nil || vr.Status != "success" || len(vr.Data.Result) == 0 {
		return 0, false
	}
	s, ok := vr.Data.Result[0].Value[1].(string)
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil
}

func humanBytes(v float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}
