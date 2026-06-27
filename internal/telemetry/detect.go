package telemetry

import (
	"regexp"
	"strings"

	"xenon/internal/model"
)

// Detect returns the highest-priority matching detection rule for a signature.
// Rules must be pre-sorted highest-priority-first (content.LoadBundled does this).
func Detect(sig model.Signature, rules []model.DetectionRule) (model.DetectionRule, bool) {
	for _, r := range rules {
		if matches(sig, r.Match) {
			return r, true
		}
	}
	return model.DetectionRule{}, false
}

func matches(sig model.Signature, m model.MatchSpec) bool {
	if m.ChassisMfgName != "" && !containsFold(sig.Vendor, m.ChassisMfgName) {
		return false
	}
	if m.ChassisModel != "" && !containsFold(sig.Model, m.ChassisModel) {
		return false
	}
	if m.SoftwareVersionRegex != "" {
		re, err := regexp.Compile(m.SoftwareVersionRegex)
		if err != nil || !re.MatchString(sig.Version) {
			return false
		}
	}
	for _, need := range m.SupportsModels {
		if !contains(sig.SupportedModels, need) {
			return false
		}
	}
	return true
}

func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
