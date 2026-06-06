package harness

import "fmt"

// dependencyCategory is the HarnessRule category SPEC §8.6 / §11.4 assigns to a
// translated layer violation.
const dependencyCategory = "dependency"

// LayerViolationInput is the subset of the Knowledge Engine's LayerViolation
// (SPEC §8.6) the enforcer needs to translate a prohibited cross-layer
// dependency into a HarnessRule violation. It is declared here so harness (Tier
// 2) does not import the knowledge package; the wiring layer maps
// knowledge.LayerViolation into this shape.
type LayerViolationInput struct {
	// FromPath / FromLayer identify the depending node.
	FromPath  string
	FromLayer string
	// ToPath / ToLayer identify the depended-upon node.
	ToPath  string
	ToLayer string
}

// TranslateLayerViolations converts the Knowledge Engine's layer-violation
// output into dependency-category HarnessRule violation records (SPEC §8.6,
// §11.4). Each becomes an error-severity violation: a prohibited dependency
// direction is an architectural issue, not mere debt. The synthetic rule id is
// derived from the layer pair so GC dedup groups all violations of the same
// prohibited direction.
func TranslateLayerViolations(layerViolations []LayerViolationInput) []Violation {
	out := make([]Violation, 0, len(layerViolations))
	for _, lv := range layerViolations {
		ruleID := fmt.Sprintf("layer-dependency:%s->%s", lv.FromLayer, lv.ToLayer)
		out = append(out, Violation{
			RuleID:   ruleID,
			Name:     fmt.Sprintf("layer dependency %s -> %s", lv.FromLayer, lv.ToLayer),
			Category: dependencyCategory,
			Severity: SeverityError,
			Summary: fmt.Sprintf(
				"%s (%s) may not depend on %s (%s)",
				lv.FromPath, lv.FromLayer, lv.ToPath, lv.ToLayer,
			),
			FixHint:       fmt.Sprintf("remove the dependency from %s to %s", lv.FromPath, lv.ToPath),
			AffectedFiles: dedupeFiles(lv.FromPath, lv.ToPath),
		})
	}
	return out
}

// dedupeFiles returns the non-empty unique file paths in order.
func dedupeFiles(paths ...string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}
