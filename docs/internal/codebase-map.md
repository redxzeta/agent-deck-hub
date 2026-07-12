# Agent Deck codebase map

This document is a navigation guide for maintainers and coding agents. It
describes current implementation boundaries and points to canonical sources;
it is not a replacement for user documentation or feature specifications.

## System shape

Agent Deck has one main executable with three related surfaces:

1. `cmd/agent-deck/main.go` extracts global profile configuration and dispatches
   subcommands implemented in `cmd/agent-deck/*_cmd.go`.
2. With no terminating subcommand, it creates `internal/ui.Home` and runs the
   Bubble Tea TUI. `Home` owns the interactive in-memory session/group view and
   coordinates background status updates.
3. `agent-deck web` starts `internal/web.Server`. Normal web mode runs beside
   the TUI; `web --no-tui` skips Bubble Tea. Mutating HTTP handlers use the
   `web.SessionMutator` interface, implemented by `internal/ui.WebMutator`, so
   CLI, TUI, and web behavior converge on the same session operations.

The durable source of profile state is SQLite through `internal/statedb`.
`internal/session.Storage` maps database rows to live `session.Instance`
objects and group data. Managed agent processes live in tmux and are controlled
through `internal/tmux`; database status is a persisted observation, not the
process itself.

## Major subsystems

| Area | Responsibility | Start here |
|---|---|---|
| Executable and CLI | Profile selection, command routing, CLI parsing, TUI/web bootstrap | `cmd/agent-deck/main.go` |
| Interactive UI | Bubble Tea state/update/view loop, navigation, dialogs, status polling | `internal/ui/home.go` |
| Session domain | Instances, lifecycle, tools, hooks, groups, profiles, config, conductor, remote and worktree behavior | `internal/session` |
| Hub domain | Strict XDG inventory, ordered host/service models, snapshot availability semantics, bounded Linux host probing | `internal/hub` |
| Process transport | tmux creation, attachment, commands, capture, status and platform behavior | `internal/tmux` |
| Hub process transport | Context-aware bounded local execution and fixed-template OpenSSH argv construction | `internal/hub/runner.go`, `internal/hub/ssh.go` |
| Persistence | SQLite connection, schema, migrations, row operations, compatibility migration | `internal/statedb` and `internal/session/storage.go` |
| Web surface | HTTP APIs, auth/bind safety, snapshots, assets, WebSocket/push behavior | `internal/web/server.go` |
| Web mutations | Adapts HTTP mutations to the UI/session model; hydrates state explicitly in headless mode | `internal/ui/web_mutator.go` |
| Agent integrations | Tool-specific launch/resume IDs, hooks, MCP and skill/plugin config | `internal/session/*claude*`, `*codex*`, `*gemini*`, `*opencode*` |
| MCP pooling | Shared MCP processes, HTTP/socket proxies, respawn and scope management | `internal/mcppool` |
| Conductor/watchers | Fleet orchestration metadata, inbox/outbox, external event adapters and routing | `internal/session/conductor*`, `internal/watcher` |
| VCS/worktrees | Git/Jujutsu detection, creation, merge/finish and safety checks | `internal/git`, `internal/jujutsu`, `internal/vcsbackend` |
| Web frontend | Embedded HTML/JS/CSS and web unit/E2E suites | `internal/web/static`, `tests/web` |

Smaller packages generally provide narrow cross-cutting services: logging,
safe file operations, platform detection, terminal launching, feedback,
updates, resource statistics, path safety, and test fixtures. Inspect imports
and neighboring tests before changing one; several are used by both CLI and
long-running processes.

## Core data flows

### Start or resume a session

```text
CLI/TUI/web request
  -> session.Instance construction or stored-state hydration
  -> tool-specific command/config resolution in internal/session
  -> tmux session/process creation in internal/tmux
  -> session IDs and metadata discovered by hooks/watchers
  -> internal/session.Storage
  -> internal/statedb SQLite profile database
  -> TUI/web snapshots and status updates
```

Tool behavior is not a single generic path. Each supported agent can have its
own command construction, resume-token discovery, hooks, scratch configuration,
MCP behavior, and tests. Preserve those distinctions when adding shared logic.

### Load and save durable state

`session.NewStorageWithProfile` resolves the effective profile, creates its
private directory, opens `state.db`, runs migrations, and imports a legacy
`sessions.json` only when appropriate. `Storage` converts between SQLite DTOs,
`InstanceData`, and live `Instance` values. SQLite uses WAL and supports
multiple processes; in-process mutexes alone are not sufficient protection.

Schema changes must update all of the following as one change:

- migration/schema and row operations in `internal/statedb`;
- serialization/hydration in `internal/session/storage.go`;
- migration and round-trip tests;
- `docs/internal/state-db-schema.md`.

### Status and UI refresh

Live status is assembled from tmux/process probes, tool hooks, persisted state,
and background watchers. `internal/sessionstatus` defines shared status
semantics, while the TUI coordinates polling and rendering. Avoid treating a
single stored status field as authoritative without tracing its writer and the
refresh path.

### Web reads and mutations

`internal/web.Server` serves snapshots through a `MenuDataLoader`. Mutations go
through the injected `SessionMutator`; production wiring uses
`ui.WebMutator`. In live-TUI mode, the Tea-owned model is already populated. In
headless mode, `WebMutator` serializes a hydrate-mutate-persist transaction so
concurrent handlers do not overwrite each other. Changes here require both
handler-level and mutation/concurrency tests.

## Where to make changes

| Change | Inspect together | Minimum focused validation |
|---|---|---|
| Add or change a CLI command | command file, dispatch/help, session operation | `go test ./cmd/agent-deck -run TestRelevant -count=1` |
| Change TUI behavior | `home.go`, relevant dialog/panel, session method | `go test ./internal/ui -run TestRelevant -count=1` |
| Change lifecycle or tool launch | session adapter, tmux calls, storage/hook behavior | focused `internal/session` and `internal/tmux` tests with `-race` |
| Change persistence | statedb migration/queries, storage mapping, schema doc | focused `internal/statedb` and `internal/session` round-trip tests |
| Change web API behavior | server/handler, API types, `WebMutator`, frontend caller | `go test ./internal/web ./internal/ui -run TestRelevant -count=1` |
| Change frontend code/styles | embedded sources, web tests, generation pipeline | `make css-verify`, `make test-web-unit`, relevant E2E tests |
| Change tmux/session survival | session lifecycle plus tmux/platform files | persistence-focused race tests and `scripts/verify-session-persistence.sh` |
| Change watchers/conductor | adapter/router or conductor/inbox paths, durable layout | focused watcher/session tests with race detection |
| Change release/build assets | Makefile, workflows, embed/generation inputs | matching release tests plus build and drift checks |

Tests usually live beside production code. Broader suites are under
`internal/integration`, `internal/tuitest`, and `tests`. Reuse helpers in
`internal/testutil`, especially HOME/XDG, tmux, profile, clock, and fixture
isolation. Never point tests at a developer's actual profile database or tmux
sessions.

## Validation tiers

1. Format and focused unit tests while iterating.
2. Run neighboring packages when a boundary or shared type changes.
3. Run `go vet ./...`, `make lint`, `go build ./cmd/agent-deck`, and
   `git diff --check` before handoff.
4. Run `go test -race -count=1 ./...` for Go behavior changes when the required
   external tools are available. CI installs tmux and zoxide and allows a long
   timeout.
5. Use `make ci` when validating the complete pre-push contract. It runs CSS
   generation/drift checks, lint, build, the race suite, and manifest checks in
   a required serial order.

Specialized gates and their triggers are documented in
`.github/workflows/README.md`. Performance tests use `make test-perf`; web tests
use `make test-web-unit` and `make test-web-e2e`. Integration and evaluation
tests may use build tags or environmental prerequisites, so inspect the
relevant workflow before invoking them.

## Generated and canonical files

- `internal/web/static/styles.src.css` is the CSS source;
  `internal/web/static/styles.css` is generated by `make css`.
- Web JavaScript assets are bundled through `internal/web/bundle.go` and the
  `go:generate` declarations in `internal/web`.
- Watcher templates under `internal/watcher/assets` are embedded at build time.
  The repository also has source templates under `assets/watcher-templates`;
  inspect layout/tests before changing either copy.
- `internal/session/conductor_bridge.py` is the canonical embedded conductor
  bridge source.
- User-facing behavior belongs in `README.md` and `skills/agent-deck/references`;
  schema truth belongs in `docs/internal/state-db-schema.md`; feature contracts
  may live under `docs/rfc` or `docs/specs`.

Design documents and plans explain intent but can lag implementation. Confirm
behavior in source and tests before relying on them.

## Refresh checklist

When this map may be stale:

```bash
git status --short
find internal -mindepth 1 -maxdepth 1 -type d | sort
rg -n 'func main|NewHome|NewServer|NewStorageWithProfile' cmd internal
rg -n 'go:generate|go:embed|DO NOT EDIT|Code generated' .
go list ./cmd/... ./internal/...
```

Then compare `Makefile`, `lefthook.yml`, `.github/workflows/README.md`, and
`CONTRIBUTING.md`. Update this document when entry points, ownership boundaries,
critical data flows, generated-file rules, or validation commands change.
