// Package probe performs live gNMI onboarding detection. Junos gNMI Get is
// config-only, so we Subscribe once to the chassis + system state and parse the
// signature. Prototype: shells out to gnmic, which loads credentials from its own
// config (the real app would embed a gNMI client / read M5 credentials).
package probe

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"xenon/internal/model"
)

// Result is what a live probe yields: the engine signature plus the device's
// reported hostname and the address we reached it on.
type Result struct {
	Sig      model.Signature
	Hostname string
	Addr     string
}

// Probe runs a one-shot gNMI subscription against addr and extracts a signature
// from the chassis component and /system state.
func Probe(addr string) (Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// gnmic auto-loads its default config (credentials, TLS) when no --config is given.
	cmd := exec.CommandContext(ctx, "gnmic",
		"-a", addr, "--timeout", "5s",
		"-e", "json", "--format", "flat",
		"subscribe", "--mode", "once",
		"--path", "/components/component[name=Chassis]/state",
		"--path", "/system/state")
	// Junos streams a once-subscribe for the timeout window before exiting (often
	// non-zero), but the identity we need is in the first sample — so parse stdout
	// regardless and only fail if nothing identifiable came back.
	out, runErr := cmd.Output()

	r := Result{Addr: addr}
	for _, ln := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(ln, ": ")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch {
		case strings.HasSuffix(k, "/mfg-name"):
			r.Sig.Vendor = v
		case strings.HasSuffix(k, "[name=Chassis]/state/description"):
			r.Sig.Model = v
		case strings.HasSuffix(k, "/software-version"):
			r.Sig.Version = v
		case strings.HasSuffix(k, "/system/state/hostname"):
			r.Hostname = v
		}
	}
	if strings.Contains(strings.ToLower(r.Sig.Vendor), "juniper") {
		r.Sig.OS = "Junos"
	}
	if r.Hostname == "" {
		r.Hostname = addr
	}
	if r.Sig.Vendor == "" && r.Sig.Model == "" {
		if runErr != nil {
			return r, fmt.Errorf("gnmic probe of %s failed: %w", addr, runErr)
		}
		return r, fmt.Errorf("no chassis identity returned from %s", addr)
	}
	return r, nil
}
