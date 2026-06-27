package probe

import (
	"os"
	"testing"
)

// TestLiveProbe is a manual integration test against a real gNMI target. Set
// PROBE_ADDR (and GNMIC_USERNAME / GNMIC_PASSWORD); skipped otherwise.
func TestLiveProbe(t *testing.T) {
	addr := os.Getenv("PROBE_ADDR")
	if addr == "" {
		t.Skip("set PROBE_ADDR to run")
	}
	r, err := Probe(addr, Creds{Username: os.Getenv("GNMIC_USERNAME"), Password: os.Getenv("GNMIC_PASSWORD")})
	t.Logf("vendor=%q model=%q os=%q version=%q hostname=%q err=%v",
		r.Sig.Vendor, r.Sig.Model, r.Sig.OS, r.Sig.Version, r.Hostname, err)
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}
}
