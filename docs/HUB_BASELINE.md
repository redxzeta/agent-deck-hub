# Agent Deck Hub baseline

Recorded for Task 00 on 2026-07-11.

## Repository state

- Origin: `git@github.com:redxzeta/agent-deck-hub.git`
- Upstream: `git@github.com:asheshgoplani/agent-deck.git`
- Integration branch: `agent-deck-hub-main` (non-default)
- Integration HEAD before Task 00: `df9cacd914178365f3634f41fc90b512a75a6c44`
- Upstream-compatible base: `f70f19e04a44541792dfc871660bf80fd138a8ed`
- Integration branch relative to `origin/main`: one commit ahead, zero behind
- Go module: `github.com/asheshgoplani/agent-deck`
- Required toolchain: Go 1.25.12

The pre-existing integration commit adds `AGENTS.md` and
`docs/internal/codebase-map.md`. It is retained as useful contributor context.

## Update and identity seams

- `cmd/agent-deck/main.go` owns `Version`, CLI dispatch, startup update prompts,
  cached version output, TUI version wiring, and the local `update` command.
- `internal/update/update.go` owns upstream release lookup, update caching,
  verification, and installation. Its repository constant currently points to
  `asheshgoplani/agent-deck`.
- `internal/ui/update_nudge.go` renders the TUI update banner and upstream
  command text.
- Remote Agent Deck updates are separate session/remote flows and must remain
  available in Hub builds.
- `Makefile` currently emits only `build/agent-deck` and injects only
  `main.Version`; Task 01 adds the separate flavor-aware target.

## TUI integration seams

`cmd/agent-deck/main.go` constructs `ui.Home`; `internal/ui/home.go` owns the
Bubble Tea root model, keyboard routing, update loop, and view composition. A
self-contained Hub panel should be owned by `Home`, intercept keys only while
visible, use `tea.Cmd` for all network work, and preserve existing selection.

## Baseline validation

The repository is designed for Linux CI with tmux and zoxide installed. A
macOS managed-sandbox run downloaded Go 1.25.12 successfully when caches were
redirected to `/tmp`, but the full race suite was red for pre-existing
environment/platform reasons: missing tmux, sandbox UNIX-socket restrictions,
macOS sysinfo assumptions, and transcript-path fixtures resolving outside the
isolated home. Most packages, including `internal/ui` and `internal/update`,
passed.

Task 00 CI is the authoritative Linux baseline. It installs tmux and zoxide and
runs the existing full race gate on GitHub's Ubuntu runner. A red Linux baseline
blocks Task 01 and must be classified or fixed without hiding failures.
