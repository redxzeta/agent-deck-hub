// Package hub contains the configuration and domain model for Agent Deck Hub.
// It intentionally has no UI or subprocess dependencies.
package hub

import "time"

type Scope string

const (
	ScopeSystem Scope = "system"
	ScopeUser   Scope = "user"
)

type Action string

const (
	ActionStatus  Action = "status"
	ActionLogs    Action = "logs"
	ActionRestart Action = "restart"
)

// Inventory is the operator-declared Hub infrastructure inventory. Slice order
// is significant and matches declaration order in hub.toml.
type Inventory struct {
	Hosts []Host `toml:"hosts"`
}

type Host struct {
	ID       string    `toml:"id"`
	Target   string    `toml:"target"`
	Services []Service `toml:"services"`
}

type Service struct {
	ID      string   `toml:"id"`
	Unit    string   `toml:"unit"`
	Scope   Scope    `toml:"scope"`
	Actions []Action `toml:"actions"`
}

// Available represents a measured value. Available=false means the value is
// unknown; callers must not interpret Value's zero value as a measurement.
type Available[T any] struct {
	Value     T
	Available bool
}

type HostSnapshot struct {
	HostID      string
	CollectedAt time.Time
	Stale       bool
	Error       string
	Uptime      Available[time.Duration]
	Load1       Available[float64]
	MemoryUsed  Available[uint64]
	MemoryTotal Available[uint64]
	Services    []ServiceSnapshot
}

type ServiceSnapshot struct {
	ServiceID   string
	Unit        string
	ActiveState Available[string]
	SubState    Available[string]
}
