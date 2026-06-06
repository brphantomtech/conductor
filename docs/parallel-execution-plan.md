# Parallel Execution Plan (Phases 7–18)

This plan reorganizes the phases in [phases.md](./phases.md) into **waves** that can be built
concurrently in separate git worktrees without stepping on each other. The phase *definitions*
in `phases.md` remain canonical — this file only adds the *concurrency contract*: what runs in
parallel, what each worktree is allowed to touch, and the order things merge back.

## Why this works

The codebase was architected for parallel development. Three facts make it safe:

1. **All config sections already exist** — [internal/config/types.go](../internal/config/types.go)
   declares `Routing`, `Knowledge`, `Memory`, `Docs`, `HarnessRules`, `Enforcement`, `Validation`,
   `Agent`, `Server`. Later phases *consume* these fields; they do not add them.
2. **All audit event types already exist** — [internal/audit/event.go](../internal/audit/event.go)
   declares every event each phase emits. Phases *emit* existing types; they do not redefine them.
3. **The orchestrator poll loop is frozen behind seams** —
   [internal/orchestrator/seams.go](../internal/orchestrator/seams.go) exposes `EnforcerCheck`
   (Phase 12), `Classifier` (Phase 7), `DocStoreSync` (Phase 11), `MemoryPostProcessor` (Phase 9),
   `PreflightCheck`, each with a no-op default and a matching `WithX(...)` Option already present in
   [orchestrator.go](../internal/orchestrator/orchestrator.go). No phase rewrites
   [tick.go](../internal/orchestrator/tick.go).

Because of this, the only files multiple phases touch are the three **integration points** below —
and every edit there is append-only (a line added to a list), so merges are trivial.

## The three integration points (shared-file hotspots)

| File | What each phase adds | Conflict shape |
|------|----------------------|----------------|
| [cmd/conductor/cmd/start.go](../cmd/conductor/cmd/start.go) `runOrchestrator` | one `orchestrator.WithX(engine)` line + the engine's `construct` above it | two adds to the same call → trivial |
| [cmd/conductor/cmd/root.go](../cmd/conductor/cmd/root.go) `NewRootCommand` | one `root.AddCommand(newXCommand())` line | two adds to the same list → trivial |
| [internal/audit/event.go](../internal/audit/event.go) | **only if** a genuinely new event type is missing — append to the `const` block *and* the `AllEventTypes()` slice | rare; append-only |

### The integration-commit rule

> Keep all work inside your phase's own `internal/<pkg>` plus a dedicated `cmd_<phase>.go`.
> Touch the three integration points **only in the last commit** of the worktree, and keep that
> commit tiny (wiring only — no logic).

This concentrates the (already minimal) conflict surface into one predictable commit per worktree,
so the merge of a wave is a sequence of fast-forwards plus, at most, a 2-line manual resolve.

---

## Wave A — start immediately after Phase 6

Four independent engines, each a fresh `internal/` package. **No logical dependencies between
them.** This is the safest and widest fan-out.

| Phase | Package | Branch | Plugs in via |
|-------|---------|--------|--------------|
| 7 — Agent Router & Pipelines | `internal/router` | `phase-7-router` | `WithClassifier` seam |
| 8 — Validation Pipeline | `internal/validation` | `phase-8-validation` | called per-turn; `validation run` subcmd |
| 9 — Memory Manager | `internal/memory` | `phase-9-memory` | `WithMemoryPostProcessor` seam |
| 10 — Knowledge Engine | `internal/knowledge` | `phase-10-knowledge` | consumed by Enforcer + tools |

Notes:
- Phase 9 also finishes provider compaction started in Phase 3 — that edit lives in
  `internal/provider`, untouched by the other three, so it stays conflict-free within the wave.
- Phase 7 and Phase 12 do **not** collide (earlier worry retracted): both plug into seams via
  Options in `start.go`, never into `tick.go`.

**Merge order for Wave A:** any order. Suggested: 10 → 9 → 8 → 7 (lands the most-depended-on engine
first). Resolve the `start.go` / `root.go` integration commit at each merge (trivial).

---

## Wave B — after Phase 10 lands on main

| Phase | Package | Branch | Depends on |
|-------|---------|--------|------------|
| 11 — Doc Store Manager | `internal/docstore` | `phase-11-docstore` | Phase 10 (Doc nodes in graph), `WithDocStoreSync` seam |
| 12 — Harness Enforcer | `internal/harness` (extended) | `phase-12-enforcer` | Phase 10 (layer violations), `WithEnforcer` seam |
| 16 — CLI Surface Completion | `cmd/conductor` | `phase-16-cli` | Phases 5/6 only |
| 17 — Container Isolation | `internal/workspace` + infra | `phase-17-container` | Phase 5 only |

Notes:
- 11 and 12 are separate packages — safe together once 10 is in.
- 16 and 17 depend only on 5/6 (already on main), so they can actually start during Wave A if you
  have the capacity; they are grouped here only because they are lower-priority than the engines.
- 16 is the one to watch: it adds several subcommands (`workspace list/remove/open`, `dispatch`,
  `cancel`, `init`, `status`). Give each its own `cmd_<name>.go` so the only `root.go` churn is the
  `AddCommand` lines.

**Merge order for Wave B:** 12 → 11 → 17 → 16 (CLI last so its `status` snapshot can reflect every
engine merged before it).

---

## Sequential tail — do not parallelize

| Phase | Why it is a barrier |
|-------|---------------------|
| 13 — Conductor Tool Injection | Convergence point: needs the tools backed by Phases 4, 8, 9, 10, 11, 12. Build after Wave B. |
| 14 — HTTP API & WebSocket | Surfaces nearly every engine; build after 13. |
| 15 — SvelteKit Dashboard | Hard dependency on the Phase 14 API + WS contract. |
| 18 — Extensibility Surface | Freezes the `pkg/` public interfaces of all extension points; do last. |

13 may run loosely alongside the *finishing* of Wave B, but treat it as the integration milestone:
each tool it exposes (`conductor_knowledge_search`, `conductor_memory_read`, …) needs its backing
engine already merged, so it is cleaner to start it once Wave B is on main.

---

## Worktree setup

From the repo root, one worktree per active branch (Wave A example):

```powershell
git worktree add ../conductor-p7  -b phase-7-router   main
git worktree add ../conductor-p8  -b phase-8-validation main
git worktree add ../conductor-p9  -b phase-9-memory    main
git worktree add ../conductor-p10 -b phase-10-knowledge main
```

Each worktree is a full working copy on its own branch; build/test independently with
`go build ./...` and `go test ./internal/<pkg>/...`. When a phase is done:

```powershell
# inside the worktree: ensure the integration commit is last and tiny
git switch main
git pull
git merge --no-ff phase-10-knowledge   # resolve start.go/root.go if prompted (trivial)
go build ./... ; if ($?) { go test ./... }
git worktree remove ../conductor-p10
```

Rebase the still-open Wave-A branches on the new `main` after each merge so their integration
commit replays against the latest `start.go`/`root.go` — this turns every subsequent merge into a
fast-forward of the wiring lines.

## Quick reference

```
Phase 6 (done)
   │
   ├── Wave A (parallel):  7 · 8 · 9 · 10
   │
   ├── Wave B (parallel, after 10):  11 · 12 · 16 · 17
   │
   └── Tail (sequential):  13 → 14 → 15 → 18
```
