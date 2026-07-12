# Agent Deck Hub configuration

Agent Deck Hub reads infrastructure inventory from a dedicated XDG file:

```text
$XDG_CONFIG_HOME/agent-deck/hub.toml
```

When `XDG_CONFIG_HOME` is unset, the path is
`$HOME/.config/agent-deck/hub.toml`. Hub does not read inventory from Agent
Deck's `config.toml`, legacy directory, or SQLite database.

Hosts and services are displayed in the order declared:

```toml
[[hosts]]
id = "prod-web"
target = "prod-web-alias"

  [[hosts.services]]
  id = "api"
  unit = "example-api.service"
  scope = "system"
  actions = ["status", "logs"]

  [[hosts.services]]
  id = "worker"
  unit = "example-worker@blue.service"
  scope = "user"
  actions = ["status", "logs", "restart"]
```

`target` is an OpenSSH destination or configured alias. Hub treats OpenSSH
configuration as authoritative; `hub.toml` does not define keys or arbitrary
commands.

Configuration is rejected as a whole when TOML is malformed, an unknown field
is present, IDs are invalid or duplicated, a systemd unit is invalid, a scope
or action is outside the closed set, actions repeat, or an SSH target is empty,
over 255 bytes, invalid UTF-8, or contains a control character. Validation
errors identify the affected field but do not repeat target values. No remote
command or subprocess starts while configuration is loading or validating.
