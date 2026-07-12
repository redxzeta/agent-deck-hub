package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/hub"
)

const hubCLISchemaVersion = 1

type hubCLIBackend interface {
	Inventory() *hub.Inventory
	Refresh(context.Context) []hub.HostSnapshot
}

type liveHubBackend struct {
	inventory   *hub.Inventory
	coordinator *hub.Coordinator
}

func (b *liveHubBackend) Inventory() *hub.Inventory { return b.inventory }
func (b *liveHubBackend) Refresh(ctx context.Context) []hub.HostSnapshot {
	return b.coordinator.Refresh(ctx)
}

var newHubCLIBackend = func() (hubCLIBackend, error) {
	inventory, err := hub.Load()
	if err != nil {
		return nil, err
	}
	transport := hub.NewSSHTransport(hub.NewExecRunner())
	return &liveHubBackend{inventory: inventory, coordinator: hub.NewCoordinator(inventory, hub.NewLinuxProbe(transport), hub.NewSystemdManager(transport))}, nil
}

func isHubCommand(command string) bool {
	return command == "config" || command == "status" || command == "services"
}

func hubInvocationUsesReadOnlyCLI(args []string) bool {
	if len(args) == 0 {
		return true
	}
	switch args[0] {
	case "version", "--version", "-v", "help", "--help", "-h":
		return false
	default:
		return true
	}
}

func runHubCLI(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || !isHubCommand(args[0]) {
		fmt.Fprintln(stderr, "Usage: agent-deck-hub <config|status|services> [--json]")
		return 2
	}
	jsonMode := false
	for _, arg := range args[1:] {
		if arg == "--json" && !jsonMode {
			jsonMode = true
			continue
		}
		fmt.Fprintf(stderr, "Error: unknown argument %q\n", arg)
		return 2
	}
	backend, err := newHubCLIBackend()
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 2
	}
	if args[0] == "config" {
		if jsonMode {
			return writeHubJSON(stdout, hubConfigJSON{SchemaVersion: hubCLISchemaVersion, Hosts: configHosts(backend.Inventory())}, stderr)
		}
		writeHubConfigText(stdout, backend.Inventory())
		return 0
	}

	refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	snapshots := backend.Refresh(refreshCtx)
	if snapshots == nil && refreshCtx.Err() != nil {
		fmt.Fprintln(stderr, "Error: Hub refresh timed out")
		return 2
	}
	if args[0] == "status" {
		if jsonMode {
			if code := writeHubJSON(stdout, hubStatusJSON{SchemaVersion: hubCLISchemaVersion, Hosts: snapshotHosts(snapshots)}, stderr); code != 0 {
				return code
			}
		} else {
			writeHubStatusText(stdout, snapshots)
		}
		if hostSnapshotsPartial(snapshots) {
			return 1
		}
	} else {
		if jsonMode {
			if code := writeHubJSON(stdout, hubServicesJSON{SchemaVersion: hubCLISchemaVersion, Services: snapshotServices(snapshots)}, stderr); code != 0 {
				return code
			}
		} else {
			writeHubServicesText(stdout, snapshots)
		}
		if serviceSnapshotsPartial(snapshots) {
			return 1
		}
	}
	return 0
}

type hubConfigJSON struct {
	SchemaVersion int              `json:"schema_version"`
	Hosts         []configHostJSON `json:"hosts"`
}
type configHostJSON struct {
	ID       string              `json:"id"`
	Target   string              `json:"target"`
	Services []configServiceJSON `json:"services"`
}
type configServiceJSON struct {
	ID      string       `json:"id"`
	Unit    string       `json:"unit"`
	Scope   hub.Scope    `json:"scope"`
	Actions []hub.Action `json:"actions"`
}
type hubStatusJSON struct {
	SchemaVersion int                `json:"schema_version"`
	Hosts         []snapshotHostJSON `json:"hosts"`
}
type snapshotHostJSON struct {
	ID            string                 `json:"id"`
	CollectedAt   time.Time              `json:"collected_at"`
	Stale         bool                   `json:"stale"`
	Error         string                 `json:"error,omitempty"`
	Hostname      availableJSON[string]  `json:"hostname"`
	UptimeMS      availableJSON[int64]   `json:"uptime_ms"`
	Load1         availableJSON[float64] `json:"load1"`
	MemoryUsed    availableJSON[uint64]  `json:"memory_used_bytes"`
	MemoryTotal   availableJSON[uint64]  `json:"memory_total_bytes"`
	RootDiskUsed  availableJSON[uint64]  `json:"root_disk_used_bytes"`
	RootDiskTotal availableJSON[uint64]  `json:"root_disk_total_bytes"`
	LatencyMS     availableJSON[int64]   `json:"latency_ms"`
}
type hubServicesJSON struct {
	SchemaVersion int                   `json:"schema_version"`
	Services      []snapshotServiceJSON `json:"services"`
}
type snapshotServiceJSON struct {
	HostID      string                `json:"host_id"`
	ID          string                `json:"id"`
	Unit        string                `json:"unit"`
	Stale       bool                  `json:"stale"`
	Error       string                `json:"error,omitempty"`
	ActiveState availableJSON[string] `json:"active_state"`
	SubState    availableJSON[string] `json:"sub_state"`
}
type availableJSON[T any] struct {
	Available bool `json:"available"`
	Value     T    `json:"value"`
}

func configHosts(inventory *hub.Inventory) []configHostJSON {
	result := make([]configHostJSON, 0, len(inventory.Hosts))
	for _, host := range inventory.Hosts {
		item := configHostJSON{ID: host.ID, Target: host.Target, Services: make([]configServiceJSON, len(host.Services))}
		for index, service := range host.Services {
			item.Services[index] = configServiceJSON{ID: service.ID, Unit: service.Unit, Scope: service.Scope, Actions: append([]hub.Action(nil), service.Actions...)}
		}
		result = append(result, item)
	}
	return result
}

func snapshotHosts(snapshots []hub.HostSnapshot) []snapshotHostJSON {
	result := make([]snapshotHostJSON, len(snapshots))
	for index, snapshot := range snapshots {
		result[index] = snapshotHostJSON{
			ID: snapshot.HostID, CollectedAt: snapshot.CollectedAt, Stale: snapshot.Stale, Error: snapshot.Error,
			Hostname:   availableJSON[string]{snapshot.Hostname.Available, snapshot.Hostname.Value},
			UptimeMS:   availableJSON[int64]{snapshot.Uptime.Available, snapshot.Uptime.Value.Milliseconds()},
			Load1:      availableJSON[float64]{snapshot.Load1.Available, snapshot.Load1.Value},
			MemoryUsed: availableJSON[uint64]{snapshot.MemoryUsed.Available, snapshot.MemoryUsed.Value}, MemoryTotal: availableJSON[uint64]{snapshot.MemoryTotal.Available, snapshot.MemoryTotal.Value},
			RootDiskUsed: availableJSON[uint64]{snapshot.RootDiskUsed.Available, snapshot.RootDiskUsed.Value}, RootDiskTotal: availableJSON[uint64]{snapshot.RootDiskTotal.Available, snapshot.RootDiskTotal.Value},
			LatencyMS: availableJSON[int64]{snapshot.Latency.Available, snapshot.Latency.Value.Milliseconds()},
		}
	}
	return result
}

func snapshotServices(snapshots []hub.HostSnapshot) []snapshotServiceJSON {
	var result []snapshotServiceJSON
	for _, host := range snapshots {
		for _, service := range host.Services {
			errorMessage := service.Error
			if errorMessage == "" {
				errorMessage = host.Error
			}
			result = append(result, snapshotServiceJSON{
				HostID: host.HostID, ID: service.ServiceID, Unit: service.Unit,
				Stale: host.Stale, Error: errorMessage,
				ActiveState: availableJSON[string]{service.ActiveState.Available, service.ActiveState.Value},
				SubState:    availableJSON[string]{service.SubState.Available, service.SubState.Value},
			})
		}
	}
	if result == nil {
		return []snapshotServiceJSON{}
	}
	return result
}

func hostSnapshotsPartial(snapshots []hub.HostSnapshot) bool {
	for _, snapshot := range snapshots {
		if snapshot.Stale || snapshot.Error != "" {
			return true
		}
	}
	return false
}

func serviceSnapshotsPartial(snapshots []hub.HostSnapshot) bool {
	if hostSnapshotsPartial(snapshots) {
		return true
	}
	for _, host := range snapshots {
		for _, service := range host.Services {
			if service.Error != "" {
				return true
			}
		}
	}
	return false
}

func writeHubJSON(stdout io.Writer, value any, stderr io.Writer) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintf(stderr, "Error: encode Hub output: %v\n", err)
		return 2
	}
	return 0
}

func writeHubConfigText(out io.Writer, inventory *hub.Inventory) {
	for _, host := range inventory.Hosts {
		fmt.Fprintf(out, "%s  %s\n", host.ID, host.Target)
		for _, service := range host.Services {
			fmt.Fprintf(out, "  %s  %s  %s  %s\n", service.ID, service.Unit, service.Scope, strings.Join(actionsToStrings(service.Actions), ","))
		}
	}
}
func writeHubStatusText(out io.Writer, snapshots []hub.HostSnapshot) {
	for _, snapshot := range snapshots {
		state := "ok"
		if snapshot.Stale {
			state = "stale"
		} else if snapshot.Error != "" {
			state = "error"
		}
		hostname := "unknown"
		if snapshot.Hostname.Available {
			hostname = snapshot.Hostname.Value
		}
		fmt.Fprintf(out, "%s  %s  %s", snapshot.HostID, state, hostname)
		if snapshot.Error != "" {
			fmt.Fprintf(out, "  %s", snapshot.Error)
		}
		fmt.Fprintln(out)
	}
}
func writeHubServicesText(out io.Writer, snapshots []hub.HostSnapshot) {
	for _, host := range snapshots {
		for _, service := range host.Services {
			active, sub := "unknown", "unknown"
			if service.ActiveState.Available {
				active = service.ActiveState.Value
			}
			if service.SubState.Available {
				sub = service.SubState.Value
			}
			fmt.Fprintf(out, "%s  %s  %s  %s/%s", host.HostID, service.ServiceID, service.Unit, active, sub)
			if service.Error != "" {
				fmt.Fprintf(out, "  %s", service.Error)
			} else if host.Stale {
				fmt.Fprint(out, "  stale")
			} else if host.Error != "" {
				fmt.Fprintf(out, "  %s", host.Error)
			}
			fmt.Fprintln(out)
		}
	}
}
func actionsToStrings(actions []hub.Action) []string {
	result := make([]string, len(actions))
	for i, action := range actions {
		result[i] = string(action)
	}
	return result
}
