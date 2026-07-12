# Agent Deck Hub implementation plan

This document is the delivery contract for the `redxzeta/agent-deck-hub` fork.
Hub work is integrated through `agent-deck-hub-main`; the repository default
branch remains `main` until a release PR is ready.

## Invariants

- Preserve existing Agent Deck CLI, TUI, web, session, tmux, worktree, SQLite,
  cost, and remote-instance behavior.
- Keep the Go module path `github.com/asheshgoplani/agent-deck` and avoid
  reorganizing upstream packages.
- Build and install Hub as the separate `agent-deck-hub` binary. A Hub build
  must never replace itself with an upstream Agent Deck release.
- Store infrastructure inventory in the separate XDG `hub.toml`; never store
  Hub data in Agent Deck SQLite.
- Keep Bubble Tea and Lip Gloss out of `internal/hub`.
- Use native OpenSSH and configured aliases. Do not parse SSH configuration,
  install a remote daemon, or expose arbitrary configured commands.
- Actions are closed-world and validated before execution. Use
  `exec.CommandContext`, fixed argv/templates, timeouts, output bounds, and
  `sudo -n` only.
- Restart is allowlisted, confirmed, and sanitized in the audit log. No
  autonomous remediation or deployment commands are part of the MVP.

## Sequential tasks

| Task | Deliverable | Depends on |
|---|---|---|
| 00 | Baseline, issue/milestone governance, integration CI | — |
| 01 | Fork identity, separate build/install, local update refusal | 00 |
| 02 | Strict `hub.toml` configuration and domain model | 01 |
| 03 | Bounded runner and typed OpenSSH transport | 02 |
| 04 | Fixed Linux host probe and parser | 03 |
| 05 | Typed systemd status/log/restart manager | 04 |
| 06 | Coordinator, cache, and read-only CLI | 05 |
| 07 | Read-only responsive TUI overlay | 06 |
| 08 | Native SSH and bounded journal actions | 07 |
| 09 | Restart confirmation and sanitized audit | 08 |
| 10 | Optional bounded RunTrail client | 09 |
| 11 | Read-only project/deployment context | 10 |
| 12 | Release, installation, CI completion, upstream sync docs | 11 |

Each task is one issue, one isolated `feature/hub-NN-slug` branch, and one PR
into `agent-deck-hub-main`. Tasks are not parallelized. Every PR includes
focused tests, full applicable repository checks, `git diff --check`, a senior
review, and a sanitized RunTrail completion event plus handoff before the next
task starts.

## Target architecture

```text
cmd/agent-deck/hub_*.go     CLI adapter and build-flavor boundary
internal/ui/hub_*.go        Bubble Tea panel and action adapter
internal/hub/               configuration, model, runner, SSH, probes,
                            systemd, coordinator, audit, RunTrail, projects
hub.toml                    operator-owned inventory, never committed secrets
```

Core interfaces remain typed. `ServiceManager` exposes only `Status`, `Logs`,
and `Restart`; `RunStore` exposes bounded recent-run reads. Unknown metrics use
explicit availability rather than zero values. The coordinator preserves host
declaration order, bounds concurrency, prevents overlapping refreshes, retains
last-good snapshots with stale markers, and isolates per-host failures.

## Security contract

- IDs: `^[a-z0-9][a-z0-9_-]{0,63}$`
- Units: `^[A-Za-z0-9_.@:-]+\.service$`
- Scope: `system` or `user`
- Actions: `status`, `logs`, or `restart`
- SSH targets reject control characters and unsafe length; OpenSSH config is
  authoritative.
- Remote probe input and service command templates are immutable.
- Every subprocess has cancellation, timeout, and separate stdout/stderr caps.
- Logs, errors, RunTrail events, and audits omit tokens, environment values,
  private-key paths, and raw subprocess output.

## Completion gates

The MVP is complete only when both binaries build, existing sessions remain
compatible, Hub local self-update is impossible, config validation is strict,
offline hosts cannot freeze the UI, SSH/log/restart behavior is bounded and
authorized, audit and optional RunTrail behavior are isolated, Linux race tests
pass, Hub release artifacts are verified, and upstream sync is documented.

Deferred work includes Proxmox operations, embedded terminals, remote daemons,
arbitrary widgets/scripts, Hub SQLite, deployment automation, web parity,
workflow engines, Kubernetes, Redis, and autonomous remediation.
