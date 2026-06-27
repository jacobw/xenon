// Command xenon is a prototype slice of the xenon engine: it takes a
// sample device signature (stand-in for gNMI detect), matches a detection rule,
// assigns a profile, and compiles the gNMIc target config — exercising M1 +
// content + M2.a/b/c with zero external dependencies.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"xenon/internal/content"
	"xenon/internal/model"
	"xenon/internal/telemetry"
)

func main() {
	store, err := content.LoadBundled()
	if err != nil {
		fmt.Fprintln(os.Stderr, "load content:", err)
		os.Exit(1)
	}

	samples := []struct {
		name   string
		sig    model.Signature
		optIns []string
		tags   map[string]string
	}{
		{
			name: "core1.example.com",
			sig: model.Signature{
				Vendor: "Juniper Networks", Model: "MX304", OS: "Junos", Version: "23.4R1.9",
				SupportedModels: []string{"openconfig-interfaces", "openconfig-system", "openconfig-network-instance"},
			},
			optIns: []string{"qos"},
			tags:   map[string]string{"site": "dc1", "role": "core"},
		},
		{
			// Example access switch: Juniper EX4100-F-12P, Junos 23.4R2-S7.7
			// (signature as gNMI reports it from /components + /system).
			name: "sw1.lab.example.com",
			sig: model.Signature{
				Vendor: "Juniper", Model: "EX4100-F-12P", OS: "Junos", Version: "23.4R2-S7.7",
				SupportedModels: []string{"openconfig-interfaces", "openconfig-system", "openconfig-platform"},
			},
			tags: map[string]string{"site": "lab", "role": "access"},
		},
		{
			name: "mystery1",
			sig: model.Signature{
				Vendor: "Acme", Model: "X9000", OS: "AcmeOS", Version: "1.0",
				SupportedModels: []string{"openconfig-interfaces", "openconfig-system"},
			},
		},
	}

	for _, s := range samples {
		fmt.Printf("\n=== %s ===\n", s.name)

		rule, ok := telemetry.Detect(s.sig, store.DetectionRules)
		if !ok {
			fmt.Println("  no detection rule matched (not even generic) -> state: unclassified")
			continue
		}
		generic := rule.Platform.Model == "unknown"
		state := "active"
		if generic {
			state = "unclassified (generic fallback — flagged; add a detection rule)"
		}
		fmt.Printf("  matched rule:     %s (priority %d)\n", rule.ID, rule.Priority)
		fmt.Printf("  platform:         %s/%s/%s\n", rule.Platform.Vendor, rule.Platform.Family, rule.Platform.Model)
		fmt.Printf("  assigned profile: %s\n", rule.Profile)
		fmt.Printf("  lifecycle state:  %s\n", state)

		prof, exists := store.Profiles[rule.Profile]
		if !exists {
			fmt.Printf("  !! profile %q not found in content\n", rule.Profile)
			continue
		}

		dev := model.Device{
			ID: "d-" + s.name, Hostname: s.name, MgmtAddress: s.name + ":9339",
			CredentialRef: "default", Tags: s.tags, ProfileID: prof.ID,
			OptIns: s.optIns, Platform: rule.Platform, State: state,
		}

		fmt.Printf("  est series/device (planning): ~%d\n", telemetry.EstimateSeries(dev, prof))

		out, _ := json.MarshalIndent(telemetry.Compile(dev, prof, "telemetry-ro"), "  ", "  ")
		fmt.Printf("  generated gNMIc config:\n  %s\n", out)
	}
}
