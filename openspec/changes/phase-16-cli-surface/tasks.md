# Phase 16 — Atomic Tasks (CLI Surface Completion)

Phase 16 goal (per [docs/phases.md](../../../docs/phases.md)): every `conductor <subcommand>` in
SPEC §19.1 works, scoped to what is reachable without the Phase 14 control channel. SPEC §19. Wave B
worktree — one `cmd_*.go` per command group; touch `root.go` only in the final commit (see
[docs/parallel-execution-plan.md](../../../docs/parallel-execution-plan.md)). Each task is sized for
one focused session.

## 1. Workspace Commands

- [ ] 1.1 Implement `conductor workspace list` in `cmd/conductor/cmd/cmd_workspace.go`: enumerate `workspace.root`, report key/issue/path/repos (text + `--format json`).
- [ ] 1.2 Implement `conductor workspace remove <key>`: delete via the workspace manager (runs `before_remove` + §14.2 invariants); `--yes` for non-interactive confirmation.
- [ ] 1.3 Implement `conductor workspace open <key>`: resolve path, launch `$EDITOR`; fall back to printing the path when `$EDITOR` unset.
- [ ] 1.4 Unit tests (temp `workspace.root`): list reports workspaces; remove deletes via manager + gone from list; open without `$EDITOR` prints path.

## 2. Init Scaffolding

- [ ] 2.1 Implement `conductor init --profile local|team|cloud` in `cmd_init.go`: write a starter `HARNESS.md` (required fields present) and, for team/cloud, a `docker-compose.yml`/Dockerfile stub.
- [ ] 2.2 No-overwrite guard: never overwrite existing files without `--force`.
- [ ] 2.3 Unit tests: each profile scaffolds; scaffolded HARNESS.md passes `config.Validate` with placeholder secrets; no-overwrite guard honored.

## 3. Dispatch / Cancel / Status

- [ ] 3.1 Implement `conductor status` in `cmd_status.go`: print the static snapshot (config summary, workspace inventory, DB/audit reachability); `--format json`.
- [ ] 3.2 Implement `conductor dispatch <id>` and `conductor cancel <id>` in `cmd_dispatch.go`/`cmd_cancel.go`: argument parsing; perform offline-feasible work; otherwise return a structured "requires the running service (Phase 14)" message — never a silent no-op.
- [ ] 3.3 Unit tests: status static snapshot; dispatch/cancel arg parsing + the "requires running service" path.

## 4. CLI Wiring

- [ ] 4.1 **Integration commit (final, append-only):** register `newWorkspaceCommand()`, `newInitCommand()`, `newStatusCommand()`, `newDispatchCommand()`, `newCancelCommand()` in `cmd/conductor/cmd/root.go`; update `AGENTS.md` navigation for the completed CLI surface.

## 5. Verification

- [ ] 5.1 `go build ./...` and `go vet ./...` clean; repo linter passes for the new `cmd` files.
- [ ] 5.2 `go test ./cmd/...` passes; new command code coverage ≥ 70%.
- [ ] 5.3 Smoke: `conductor init --profile local` in a temp dir scaffolds a valid HARNESS.md; `conductor workspace list` and `conductor status` run against it; `conductor dispatch X` prints the "requires running service" notice.
