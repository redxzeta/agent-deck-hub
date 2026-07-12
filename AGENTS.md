# Agent Deck contributor guidance

Agent Deck is a Go application for managing AI-agent sessions. It exposes a
Bubble Tea TUI, a CLI, and an optional web UI over shared session and profile
state. Start with `README.md` for user-facing behavior and
`docs/internal/codebase-map.md` for code navigation and runtime data flows.

## Repository map

- `cmd/agent-deck`: executable entry point and CLI command dispatch.
- `internal/ui`: Bubble Tea model, dialogs, keyboard handling, and the bridge
  used by web mutations.
- `internal/session`: session lifecycle, tool adapters, profiles, groups,
  configuration, hooks, worktrees, conductor support, and persistence mapping.
- `internal/tmux`: tmux process/session control and status probes.
- `internal/statedb`: SQLite schema, migrations, and durable state access.
- `internal/web`: HTTP server, API handlers, embedded frontend assets, and push
  support.
- `internal/watcher`, `internal/mcppool`: watcher routing and shared MCP
  process management.
- `internal/testutil`, `internal/integration`, `internal/tuitest`, `tests`:
  reusable fixtures and broader test suites.

## Working rules

- Preserve unrelated user changes and inspect `git status` before editing.
- Follow existing package patterns and add focused tests beside changed code.
- Keep tests isolated from real user state. Use temporary HOME/XDG/profile
  fixtures and existing helpers under `internal/testutil`.
- Treat source, `Makefile`, `lefthook.yml`, and `.github/workflows` as the
  current truth when prose documentation disagrees.
- Update user-facing docs for behavior changes and the codebase map when entry
  points, subsystem ownership, or validation workflows change.
- Do not hand-edit generated web assets. JavaScript bundles are produced by
  `go generate`/esbuild, and `internal/web/static/styles.css` is produced from
  `styles.src.css` by `make css`.
- Be cautious around lifecycle, persistence, tmux, and concurrent web mutation
  changes. Preserve durable state, migration compatibility, and locking.
- Contributor-specific `CLAUDE.md` and `.planning/` files are intentionally
  local according to `CONTRIBUTING.md`; do not commit them as shared guidance.

## Build and validation

Use the smallest relevant check first:

```bash
go test ./internal/session -run 'TestName' -count=1
go test ./cmd/agent-deck -run 'TestName' -count=1
go test ./internal/web -run 'TestName' -count=1
```

Standard repository checks:

```bash
go fmt ./...
go vet ./...
make lint
go build ./cmd/agent-deck
go test -race -count=1 ./...
git diff --check
```

The full race suite can be slow and requires external tools used by integration
tests, notably tmux and zoxide. `make ci` mirrors the local pre-push pipeline and
also regenerates/verifies CSS. For web changes, use `make test-web-unit`, then
`make test-web-e2e` when browser-level behavior is affected. See
`.github/workflows/README.md` and `docs/internal/codebase-map.md` for specialized
gates.

## Change navigation

- CLI behavior: begin at `cmd/agent-deck/main.go`, then the matching `*_cmd.go`.
- TUI behavior: begin at `internal/ui/home.go` and the relevant panel/dialog.
- Launch, restart, resume, tool, or group behavior: begin in `internal/session`.
- Process survival or terminal behavior: inspect both `internal/session` and
  `internal/tmux`; session persistence has a dedicated CI gate.
- Stored fields or migrations: update `internal/statedb`, the session storage
  mapping, migration tests, and `docs/internal/state-db-schema.md` together.
- Web behavior: inspect `internal/web` plus `internal/ui/web_mutator.go`; the web
  API does not own a separate session model.

Keep this file concise and stable. Put detailed architecture discoveries in
`docs/internal/codebase-map.md` and link to canonical specifications rather
than duplicating them here.
