package main

import (
	"fmt"
	"sort"

	"xenon/internal/metrics"
)

// invItem is one hardware component for the Inventory tab.
type invItem struct {
	Name   string
	Type   string
	Part   string
	Serial string
	Mfg    string
	Rev    string
	Desc   string
}

// buildInventory lists the device's hardware modules (anything with a serial or
// part number) from the component-state inventory leaves (strings-as-labels). This
// is the gNMI equivalent of LibreNMS's entity-physical inventory.
func buildInventory(mc *metrics.Client, source string) []invItem {
	// leaf reads a components_component_state_<leaf> state-set and maps
	// component_name -> the leaf's string value (carried as a same-named label).
	leaf := func(name string) map[string]string {
		out := map[string]string{}
		for _, l := range mc.VectorFull(fmt.Sprintf(`components_component_state_%s{source=%q}`, name, source)) {
			if n := l.Labels["component_name"]; n != "" {
				out[n] = l.Labels[name]
			}
		}
		return out
	}
	typ, part, serial := leaf("type"), leaf("part_no"), leaf("serial_no")
	mfg, rev, desc := leaf("mfg_name"), leaf("hardware_version"), leaf("description")

	names := map[string]bool{}
	for n := range serial {
		names[n] = true
	}
	for n := range part {
		names[n] = true
	}

	out := make([]invItem, 0, len(names))
	for n := range names {
		out = append(out, invItem{
			Name: n, Type: typ[n], Part: part[n], Serial: serial[n],
			Mfg: mfg[n], Rev: rev[n], Desc: desc[n],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Name < out[j].Name
	})
	return out
}
