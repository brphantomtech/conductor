package harness

// SPEC §4.1.2 — Harness Definition.
//
// Definition is the parsed payload of HARNESS.md. Two consumers read it:
// the orchestrator (rendering prompts and selecting pipelines) and the
// configuration layer (decoding Config out of Front Matter).
//
// HarnessRules and DocRefs are exposed as zero-value defaults in Phase 2 —
// the Harness Enforcer (Phase 12) and Doc Store Manager (Phase 11) will
// populate them once their owning packages exist.
type Definition struct {
	// Source is the absolute path of the file the Definition was parsed
	// from. Set when the loader reads HARNESS.md off disk; empty when the
	// caller parsed bytes directly.
	Source string

	// FrontMatter is the YAML front matter root object as a generic map.
	// SPEC §4.1.2 names this field "config"; we keep the field name aligned
	// with its YAML/Markdown origin to avoid confusion with the typed
	// internal/config.Config that gets unmarshalled from this map.
	FrontMatter map[string]any

	// PromptTemplates maps each agent role name to the trimmed Markdown
	// section body extracted from the file. SPEC §5.2 reserves the role
	// `coder` as the implicit fallback when the body has no `## <role>`
	// headings.
	PromptTemplates map[string]string

	// HarnessRules carries the rules pulled out of FrontMatter for the
	// Harness Enforcer (Phase 12). Phase 2 leaves it nil; the Enforcer
	// performs its own decoding when it lands.
	HarnessRules []HarnessRule

	// DocRefs carries the document references pulled out of FrontMatter for
	// the Doc Store Manager (Phase 11). Phase 2 leaves it nil.
	DocRefs []DocRef
}

// HarnessRule is the Phase-2 placeholder for SPEC §4.1.8. The Harness
// Enforcer (Phase 12) will replace this with its own typed decode; we keep
// the struct here so the Definition shape never changes between phases.
//
//revive:disable-next-line:exported // SPEC §4.1.8 names this entity HarnessRule — keep the canonical name.
type HarnessRule struct {
	ID       string
	Name     string
	Category string
	Severity string
	Check    string
	FixHint  string
	AutoFix  bool
}

// DocRef is the Phase-2 placeholder for SPEC §4.1.9. The Doc Store Manager
// (Phase 11) owns the canonical type; this stub keeps the Definition shape
// stable.
type DocRef struct {
	ID       string
	Title    string
	StoreID  string
	PathOrID string
	Tags     []string
}

// BuiltinRoles lists the SPEC §4.1.4 agent roles that ship out of the box.
// Custom roles are accepted whenever they appear in routing.pipeline; this
// list is used by the validator to pre-populate the role allowlist when the
// front matter does not yet declare a routing pipeline.
var BuiltinRoles = []string{
	"planner",
	"coder",
	"verifier",
	"reviewer",
	"gc_agent",
}

// DefaultRole is the role that receives the entire body when the file has
// no `## <role>` section headings (SPEC §5.2).
const DefaultRole = "coder"
