// Package model holds the core domain types shared across modules.
// Prototype scope: just enough to exercise detection → profile → gNMIc compile.
package model

// Signature is the normalized identity extracted from a device via gNMI
// (Capabilities + Get /system + /components). Stand-in for the real detect step.
type Signature struct {
	Vendor          string   `json:"vendor"`           // chassis mfg-name
	Model           string   `json:"model"`            // chassis part-no / description
	OS              string   `json:"os"`               // from software-version
	Version         string   `json:"version"`          // from software-version
	SupportedModels []string `json:"supported_models"` // from Capabilities
}

// Platform is the detected identity recorded on a device (M1) for display/grouping.
type Platform struct {
	Vendor string `json:"vendor"`
	Family string `json:"family"`
	Model  string `json:"model"`
}

// MatchSpec — the small, deliberate condition vocabulary for detection rules.
type MatchSpec struct {
	ChassisMfgName       string   `json:"chassis_mfg_name,omitempty"`       // substring, case-insensitive
	ChassisModel         string   `json:"chassis_model,omitempty"`          // substring, case-insensitive
	SoftwareVersionRegex string   `json:"software_version_regex,omitempty"` // regex on version
	SupportsModels       []string `json:"supports_models,omitempty"`        // signature must contain all
}

// DetectionRule maps a signature pattern → platform + default profile (content kind).
type DetectionRule struct {
	ID       string    `json:"id"`
	Priority int       `json:"priority"`
	Match    MatchSpec `json:"match"`
	Platform Platform  `json:"platform"`
	Profile  string    `json:"profile"`
}

// Subscription — one OpenConfig (or native) path in a profile.
// Group is "base" | "native" | "optin:<name>"; EstSeries is a planning estimate (path-budget method).
type Subscription struct {
	Path        string   `json:"path"`
	Mode        string   `json:"mode"` // sample | on_change
	IntervalSec int      `json:"interval_sec,omitempty"`
	Group       string   `json:"group"`
	EstSeries   int      `json:"est_series,omitempty"`
	Drop        []string `json:"drop,omitempty"` // collector-side leaf-name substrings to filter out. Junos won't honor subset/leaf subscription paths (it streams the whole container), so unwanted cardinality (e.g. per-queue "out-queue" counters) is dropped in the gNMIc processor instead. EstSeries should reflect the post-drop count.
}

// Profile — a platform/capability-keyed bundle of subscriptions (content kind).
type Profile struct {
	ID            string         `json:"id"`
	Subscriptions []Subscription `json:"subscriptions"`
}

// Device — the M1 inventory record (surrogate ID; hostname = the `device` label).
type Device struct {
	ID            string            `json:"id"`
	Hostname      string            `json:"hostname"`
	MgmtAddress   string            `json:"mgmt_address"`
	CredentialRef string            `json:"credential_ref"`
	Tags          map[string]string `json:"tags,omitempty"`    // optional operator grouping tags
	ProfileID     string            `json:"profile_id"`
	OptIns        []string          `json:"opt_ins,omitempty"` // enabled opt-in groups, e.g. "qos"
	Platform      Platform          `json:"platform"`
	State         string            `json:"state"`
}

// AlertRule — an alarm rule (content kind, M3.a). Expr is PromQL returning a
// vector; each result series is a firing alarm instance. Summary is a template
// over the series labels + {{value}}.
type AlertRule struct {
	ID       string `json:"id"`
	Severity string `json:"severity"` // critical | warning | info
	Summary  string `json:"summary"`
	Expr     string `json:"expr"`
}
