# Go Conventions

This document captures the project-specific Go conventions that go beyond standard `gofmt` /
`go vet` discipline. Where this document is silent, follow Effective Go and the
[Google Go Style Guide](https://google.github.io/styleguide/go/).

---

## 1. Naming

### Packages

- Package names are short, all-lowercase, no underscores: `harness`, `knowledge`, `memory`.
- The package name is the import-path leaf — never `package harnesspkg` or `package harness_v2`.
- One package per directory. No `internal/foo/v2/` style versioning inside the project.

### Types

- Public types use full words: `KnowledgeNode`, `RunAttempt`, `ProviderConfig`. No `KnowNode`,
  no `RunAtt`.
- Interfaces are named for the role, not the implementer: `TrackerAdapter`, not `Tracker` or
  `LinearAdapter`.
- Single-method interfaces use the `-er` suffix when natural: `Reader`, `Closer`, `Validator`.
- Multi-method interfaces describe the role: `ProviderAdapter`, `KnowledgeStore`,
  `ToolPlugin`.

### Variables and Receivers

- Receivers are short (1–2 letters): `func (s *Session) Close()`, `func (ke *KnowledgeEngine)
  Index()`.
- Local variables are short in short scopes (`i`, `err`, `ctx`) and longer when their
  scope is wider.
- No Hungarian notation; no `pConfig`, no `strProjectID`.
- `ID` is uppercase in exported identifiers (`ProjectID`, `IssueID`), not `Id`.
- Acronyms keep their case: `URL`, `HTTP`, `JSON`, `SQL`, `API`. Use `JSONPayload`, never
  `JsonPayload`. Lowercased: `urlPath`, `httpClient`.

### Constants

- Enum-like constants use a typed string or int with grouped `const`:

  ```go
  type Severity string
  const (
      SeverityWarning  Severity = "warning"
      SeverityError    Severity = "error"
      SeverityBlocking Severity = "blocking"
  )
  ```

- No bare `const SEVERITY_WARNING = "warning"`. The type is what makes a value an enum.

### Files

- One primary type per file when the file represents a major concept. `harness/loader.go`
  contains `Loader`. `harness/rule.go` contains `Rule`.
- Test files mirror: `loader.go` ↔ `loader_test.go`.
- Maximum file length: target 500 lines (warning at this threshold per SPEC §5.3.11 example).
  Split when exceeded.

---

## 2. Error Handling

### Wrapping

- Always wrap errors when crossing a function boundary that adds context:

  ```go
  if err := s.db.Insert(ctx, evt); err != nil {
      return fmt.Errorf("audit: insert event %s: %w", evt.ID, err)
  }
  ```

- Use `%w` (not `%v`) for wrapped errors so `errors.Is` and `errors.As` work upstream.
- The wrapping prefix is `<package>: <action>: <ids relevant to context>`. No "failed to",
  no "error during" — the word `error` is implicit.

### Sentinel Errors

- SPEC §23 defines a fixed registry of error classifications. Each one is a sentinel exported
  by the relevant package:

  ```go
  // internal/harness/errors.go
  var (
      ErrMissingHarnessFile      = errors.New("missing_harness_file")
      ErrHarnessParse            = errors.New("harness_parse_error")
      ErrHarnessFrontMatterShape = errors.New("harness_front_matter_not_a_map")
      ErrTemplateParse           = errors.New("template_parse_error")
      ErrTemplateRender          = errors.New("template_render_error")
  )
  ```

- The string value matches the SPEC §23 identifier exactly so it appears verbatim in audit
  logs and API responses.
- Higher layers detect specific failure modes with `errors.Is(err, harness.ErrTemplateRender)`.

### When to Return vs. Log

- Library / Tier 0–4 code returns errors; it does not log them.
- Tier 5 (api) and Tier 6 (cmd) decide whether to log + respond or log + exit.
- Background workers (orchestrator poll loop, knowledge indexer, memory consolidator) are the
  exception — they own their lifecycle and log at the worker level, never return errors to no
  one.

### Panics

- No panics in production code paths. `panic` is reserved for "this cannot happen" invariant
  violations during construction (e.g., a bad regex in a `MustCompile`).
- Tests may use `require.NoError` (which calls `t.Fatal`) freely.

---

## 3. Interface Design

### Define Interfaces at the Consumer

- Interfaces live in the package that uses them, not the package that implements them:

  ```go
  // internal/orchestrator/orchestrator.go
  type tracker interface {
      FetchCandidateIssues(ctx context.Context) ([]Issue, error)
      // ...
  }
  ```

- Exception: cross-cutting public extension points (`provider.Adapter`, `tracker.Adapter`,
  `knowledge.Store`, `docstore.Backend`, `tool.Plugin`) are defined in the implementer's home
  package because they are the contract third parties implement (SPEC §20).

### Keep Interfaces Small

- Prefer single-method interfaces when one method suffices.
- Multi-method interfaces are acceptable for genuine extension points where every method is
  required (e.g., `ProviderAdapter` has 5 methods because all 5 are needed to run a session).
- "Big interface" smell: if an interface has 10+ methods and not every implementer needs all of
  them, split it.

### Context Always Comes First

- Every interface method that does I/O takes `context.Context` as its first argument:
  ```go
  type Reader interface {
      Read(ctx context.Context, id string) (Item, error)
  }
  ```
- Pure-computation methods may omit `ctx`. When in doubt, include it.

### Concrete Types in the API

- Function and method signatures expose concrete types when the consumer benefits from the
  full surface area (`*Workspace`, `*RunAttempt`).
- Interfaces are introduced when there are multiple implementations (real + fake for tests, or
  multiple production backends).
- Resist the urge to interface-everything "for testability." Concrete types with dependency
  injection at construction are usually clearer.

---

## 4. Concurrency

- Every long-running goroutine is owned by a struct. No package-level goroutines.
- The owner exposes `Start(ctx)` and stops via `ctx.Done()`. No separate `Stop()` method
  unless ordering matters.
- Channels are owned by senders. Senders close channels; receivers do not.
- Mutexes guard small critical sections. If you find yourself holding a mutex across an I/O
  call, the design is wrong — restructure to release before I/O.
- The `OrchestratorRuntimeState` (SPEC §4.1.11) is the one place where serialized access to
  shared mutable state is centralized. All other state is goroutine-local or channel-passed.

---

## 5. Logging

- Use `zerolog` consistently. Loggers are passed in at construction:
  ```go
  func NewLoader(log zerolog.Logger, ...) *Loader { return &Loader{log: log, ...} }
  ```
- Standard log keys: `issue_id`, `issue_identifier`, `agent_role`, `turn_index`, `session_id`,
  `event_type`, `duration_ms`. Use these names exactly so log analysis tools work.
- `log.Info()` for routine events; `log.Warn()` for recoverable anomalies; `log.Error()` for
  things the operator must see; `log.Debug()` for verbose trace-level detail.
- Never log secrets. Provider API keys, tracker tokens, and doc-store auth are zeroed before
  any struct is logged. SPEC §21.1 is the rule.

---

## 6. Testing

- Table-driven tests are the default for any function with branching:

  ```go
  func TestNormalizeWorkspaceKey(t *testing.T) {
      tests := []struct {
          name string
          in   string
          want string
      }{
          {"alphanumeric", "ABC-123", "ABC-123"},
          {"slash replaced", "feature/abc", "feature_abc"},
      }
      for _, tt := range tests {
          t.Run(tt.name, func(t *testing.T) {
              got := NormalizeWorkspaceKey(tt.in)
              require.Equal(t, tt.want, got)
          })
      }
  }
  ```

- `require` for stop-on-first-failure assertions; `assert` only when subsequent assertions
  remain meaningful after a failure.
- Mocks: hand-written fakes preferred over generated mocks. testify `mock` package is allowed
  but only for cross-package boundary tests (e.g., faking a `tracker.Adapter` in
  orchestrator tests).
- Integration tests use the build tag `//go:build integration` and live in
  `internal/<pkg>/integration_test.go`. They are not run by `make test` (which runs unit
  tests only). The CI pipeline runs them separately.

---

## 7. Documentation

- Every exported identifier has a doc comment that starts with the identifier's name.
  ```go
  // Loader parses HARNESS.md from disk or a Doc Store and validates it against
  // the schema in SPEC §5.
  type Loader struct { ... }
  ```
- Doc comments are full sentences. They state what, not how.
- `doc.go` in each package contains a package-level doc comment. The `internal/<pkg>/doc.go`
  files seeded by the scaffold are starting points — they will be expanded as packages grow.
- Avoid in-body comments unless the why is non-obvious (a workaround, an invariant, a hidden
  constraint). Don't comment _what_ the code does — names and types do that.

---

## 8. Imports

- `goimports` enforces grouping: stdlib, third-party, project-local.
- Project-local imports use the full module path: `github.com/conductor-sh/conductor/internal/audit`.
- No dot imports. No relative imports.
- Import aliasing only when there's a genuine collision (e.g., two `client` packages); do
  not alias for vanity.
