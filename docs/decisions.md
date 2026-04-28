# Architecture Decision Records

Each ADR records one technology choice from SPEC §3.1, the alternatives considered, and the
rationale. Status of all ADRs in this document is **Accepted** unless noted. They reflect the
state of the spec and may evolve as the implementation progresses.

---

## ADR-0001: Backend Runtime — Go 1.23+

**Decision.** The backend is written in Go 1.23 or later.

**Alternatives considered.**

- Rust: best-in-class performance and memory safety, but slower compile/iterate cycle and a
  steeper hiring bar than Go.
- Node/TypeScript: shared language with the frontend, but worse single-binary story and weaker
  concurrency primitives for a long-running orchestrator.
- Python: rich AI/ML ecosystem, but the deployment story (single binary, low memory, predictable
  goroutine concurrency) does not match the requirements.

**Rationale.** Conductor is a long-running service that supervises subprocesses, holds many
concurrent streams (one per running agent), serves an HTTP+WS surface, and ships as a single
local binary. Goroutines model agent execution naturally. The standard library covers HTTP,
filesystem watching glue, and process supervision out of the box. Cross-compilation is trivial.

---

## ADR-0002: Package Manager (Backend) — Go Modules

**Decision.** Use Go modules natively. No `make`-driven vendor directory, no third-party
package manager.

**Rationale.** Go modules are the standard since Go 1.11 and are sufficient for our needs.
Adding tools like `dep` or vendoring would create friction without payoff.

---

## ADR-0003: HTTP Server — `net/http` + Chi v5

**Decision.** Use `net/http` for the HTTP transport and Chi v5 for routing.

**Alternatives considered.** Gin (more middleware ecosystem, but heavier and less idiomatic),
Echo (similar tradeoff), Fiber (fasthttp-based, breaks `net/http` interop).

**Rationale.** Chi is a thin, idiomatic router that composes with the standard `http.Handler`
interface. It has zero non-stdlib dependencies, supports nested routers and middleware
naturally, and stays close to Go's standard library style.

---

## ADR-0004: WebSocket — Gorilla WebSocket

**Decision.** Use `github.com/gorilla/websocket` for the dashboard real-time channel.

**Alternatives considered.** `nhooyr.io/websocket` (modern, context-aware), `golang.org/x/net/websocket`
(deprecated).

**Rationale.** Gorilla is battle-tested, used in production at scale, and has the largest
community of helpers and recipes. The "Gorilla project archived" status was reversed; the
project is active again and remains the de facto choice.

---

## ADR-0005: CLI — Cobra + Viper

**Decision.** Cobra for command/flag parsing; Viper for configuration loading.

**Rationale.** This is the canonical Go CLI stack (kubectl, hugo, cockroachdb, gh use Cobra).
Viper integrates with Cobra natively and provides the env-var + flag + config-file precedence
chain that SPEC §6.1 specifies.

---

## ADR-0006: Database (default) — SQLite via `modernc.org/sqlite`

**Decision.** Default storage is SQLite, accessed via the pure-Go `modernc.org/sqlite` driver.

**Alternatives considered.** `mattn/go-sqlite3` (CGO; faster but breaks single-binary cross-compile).

**Rationale.** The "single binary, no external deps" deployment profile (SPEC §3.2 Profile A)
requires CGO-free SQLite. `modernc.org/sqlite` is a pure-Go translation of the SQLite C source
and is fast enough for the local profile.

---

## ADR-0007: Database (optional) — PostgreSQL via `pgx`

**Decision.** PostgreSQL is the optional team/cloud profile backend; access via `jackc/pgx`.

**Rationale.** `pgx` is the most performant Postgres driver for Go and exposes Postgres-specific
features (LISTEN/NOTIFY, COPY) that may be useful later. Used directly, not through `database/sql`,
to retain those features.

---

## ADR-0008: Vector Store (default) — SQLite + sqlite-vec

**Decision.** Default vector store is SQLite with the `sqlite-vec` extension, in the same database
file as relational state.

**Alternatives considered.** Qdrant only (requires separate process), pgvector (couples vector
store to Postgres choice), in-process FAISS (CGO).

**Rationale.** Co-locating vector and relational data simplifies the local profile to a single
file. `sqlite-vec` is a small, well-maintained extension that loads via `LOAD_EXTENSION`.

---

## ADR-0009: Vector Store (optional) — Qdrant

**Decision.** Qdrant is the optional team/cloud profile vector store.

**Rationale.** Qdrant scales beyond what SQLite-vec handles efficiently (millions of vectors),
supports filterable HNSW indices, and runs as a small Docker service.

---

## ADR-0010: Template Engine — Liquid Go

**Decision.** Use `github.com/osteele/liquid` for `HARNESS.md` template rendering.

**Rationale.** SPEC §16.2 requires Liquid syntax for compatibility with Symphony's harness
templates. `osteele/liquid` is a faithful port with strict variable checking, which the spec
mandates for `template_render_error` semantics.

---

## ADR-0011: YAML Parsing — go-yaml v3

**Decision.** Use `gopkg.in/yaml.v3` for YAML front-matter and config parsing.

**Rationale.** v3 supports node-level decoding (preserves source positions, handy for error
messages on `HARNESS.md`) and is the most widely-used YAML library in the Go ecosystem.

---

## ADR-0012: Process Management — `os/exec` + Goroutines

**Decision.** Each agent run is a goroutine that owns subprocesses via `os/exec`. Docker is
opt-in per SPEC §14.3.

**Rationale.** Goroutines are sufficient isolation for the local profile and trusted environments.
Docker adds a real isolation boundary for air-gapped or regulated deployments without forcing
the cost on every user.

---

## ADR-0013: Filesystem Watch — fsnotify

**Decision.** Use `github.com/fsnotify/fsnotify` for filesystem events.

**Rationale.** Cross-platform (inotify/kqueue/ReadDirectoryChangesW), the standard in the Go
ecosystem. Required for `HARNESS.md` hot-reload (SPEC §6.3) and incremental knowledge indexing
(SPEC §8.3).

---

## ADR-0014: Frontend Runtime — Bun

**Decision.** Use Bun as the frontend runtime, package manager, and bundler.

**Alternatives considered.** Node + npm/pnpm (slower install, slower bundler), Deno (smaller
ecosystem for SvelteKit).

**Rationale.** Bun is dramatically faster than Node for install + dev server start, and is
fully compatible with SvelteKit. It also avoids the Node version-management overhead.

---

## ADR-0015: Frontend Framework — SvelteKit

**Decision.** SvelteKit for the dashboard.

**Alternatives considered.** React + Tanstack Query (familiar, larger ecosystem), Vue + Nuxt,
Solid Start.

**Rationale.** Per SPEC's note: SvelteKit compiles to vanilla JS without virtual-DOM overhead,
producing 30–50% smaller bundles and faster reactivity for the streaming dashboard. Svelte
stores model the WebSocket-driven state cleanly. Learning curve from React is 1–2 days. If
React becomes a hard requirement later, Vite + React 19 + Tanstack Query is the documented
fallback.

---

## ADR-0016: Frontend Styling — Tailwind CSS v4

**Decision.** Tailwind CSS v4 utility-first styling.

**Rationale.** Zero-runtime cost, pairs naturally with Svelte's scoped styles, no separate
component library lock-in. v4 has better Vite integration than v3.

---

## ADR-0017: Frontend Build — Vite (via SvelteKit) + Bun

**Decision.** Vite is the bundler (transitively via SvelteKit); Bun runs it.

**Rationale.** Vite's HMR is sub-second; Bun runs it faster than Node. SvelteKit's defaults are
already correct.

---

## ADR-0018: Frontend State — Svelte Stores + WebSocket

**Decision.** Local state via Svelte's reactive stores; cross-component state pushed by the
backend over a single WebSocket per client.

**Rationale.** No Redux/Zustand/Pinia equivalent needed — Svelte stores are reactive primitives.
Polling is replaced by server-pushed events (SPEC §18.2), which reduces backend load and
gives sub-second updates on agent state changes.

---

## ADR-0019: Embedding Delivery — Go `embed`

**Decision.** Frontend assets are embedded into the Go binary via the `embed` package.

**Rationale.** SPEC §3.2 Profile A is "single binary, no external deps." Embedding the built
SvelteKit assets satisfies that. The build pipeline runs `bun run build` then `go build`.

---

## ADR-0020: Codebase Parsing — go-tree-sitter

**Decision.** Use `github.com/smacker/go-tree-sitter` for AST parsing.

**Alternatives considered.** Language-specific parsers (Go's `go/parser`, TypeScript's compiler
API via subprocess), regex extraction.

**Rationale.** tree-sitter has grammars for every language SPEC §8.2 lists, in one tool, with
incremental parsing built in. The dependency-graph extraction (SPEC §8.6) requires real ASTs.

---

## ADR-0021: Cron Scheduling — robfig/cron

**Decision.** `github.com/robfig/cron/v3` for the GC enforcement schedule.

**Rationale.** The de facto Go cron library. v3 supports the standard 5-field expression that
SPEC §5.3.12 specifies (`gc_schedule_cron: "0 2 * * *"`).

---

## ADR-0022: Logging — zerolog

**Decision.** `github.com/rs/zerolog` for structured logging.

**Alternatives considered.** `log/slog` (stdlib, since Go 1.21), `uber-go/zap`.

**Rationale.** zerolog has the lowest allocation cost and the most ergonomic structured-log
API. `slog` is a strong alternative and we may migrate later; for now, zerolog is the lower-risk
choice given its maturity. Reconsidered if `slog` ecosystem catches up.

---

## ADR-0023: Testing — Go `testing` + testify + testcontainers-go

**Decision.** Standard `testing` package with `stretchr/testify` for assertions/mocks and
`testcontainers/testcontainers-go` for real-database integration tests.

**Rationale.** testify reduces assertion boilerplate. testcontainers gives integration tests a
real Postgres / Qdrant / etc. without polluting the dev machine. Spec §3.1 names these explicitly.

---

## ADR-0024: Container (optional) — Docker + Docker Compose

**Decision.** Docker Compose for the team/self-hosted profile (SPEC §3.2 Profile B).

**Rationale.** Compose is the lowest-friction multi-service orchestrator for small teams. The
cloud profile (Profile C) targets Kubernetes; Compose is the bridge.
