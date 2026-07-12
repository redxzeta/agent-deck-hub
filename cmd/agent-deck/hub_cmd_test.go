package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/hub"
)

type fakeHubCLIBackend struct {
	inventory *hub.Inventory
	snapshots []hub.HostSnapshot
}

func (b *fakeHubCLIBackend) Inventory() *hub.Inventory                  { return b.inventory }
func (b *fakeHubCLIBackend) Refresh(context.Context) []hub.HostSnapshot { return b.snapshots }

func withHubCLIBackend(t *testing.T, backend hubCLIBackend, err error) {
	t.Helper()
	old := newHubCLIBackend
	newHubCLIBackend = func() (hubCLIBackend, error) { return backend, err }
	t.Cleanup(func() { newHubCLIBackend = old })
}

func cliFixture() *fakeHubCLIBackend {
	return &fakeHubCLIBackend{
		inventory: &hub.Inventory{Hosts: []hub.Host{
			{ID: "first", Target: "a", Services: []hub.Service{{ID: "api", Unit: "api.service", Scope: hub.ScopeSystem, Actions: []hub.Action{hub.ActionStatus}}}},
			{ID: "second", Target: "b"},
		}},
		snapshots: []hub.HostSnapshot{
			{
				HostID: "first", CollectedAt: time.Unix(1, 0).UTC(),
				Hostname: hub.Available[string]{Value: "one", Available: true},
				Services: []hub.ServiceSnapshot{{
					ServiceID: "api", Unit: "api.service",
					ActiveState: hub.Available[string]{Value: "active", Available: true},
					SubState:    hub.Available[string]{Value: "running", Available: true},
				}},
			},
			{HostID: "second", CollectedAt: time.Unix(2, 0).UTC(), Error: "host probe failed"},
		},
	}
}

func TestHubConfigJSONIsVersionedAndOrdered(t *testing.T) {
	withHubCLIBackend(t, cliFixture(), nil)
	var stdout, stderr bytes.Buffer
	if code := runHubCLI(context.Background(), []string{"config", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	var output hubConfigJSON
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.SchemaVersion != 1 || len(output.Hosts) != 2 || output.Hosts[0].ID != "first" || output.Hosts[1].ID != "second" || output.Hosts[0].Services[0].ID != "api" {
		t.Fatalf("output = %#v", output)
	}
}

func TestHubStatusPartialJSONUsesExitOneAndStdout(t *testing.T) {
	withHubCLIBackend(t, cliFixture(), nil)
	var stdout, stderr bytes.Buffer
	if code := runHubCLI(context.Background(), []string{"status", "--json"}, &stdout, &stderr); code != 1 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if stderr.Len() != 0 || !strings.Contains(stdout.String(), `"schema_version": 1`) || !strings.Contains(stdout.String(), `"host probe failed"`) {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestHubStatusJSONPreservesAvailableZeroValues(t *testing.T) {
	fixture := cliFixture()
	fixture.snapshots = []hub.HostSnapshot{{
		HostID: "idle", Load1: hub.Available[float64]{Value: 0, Available: true},
		Latency: hub.Available[time.Duration]{Value: 0, Available: true},
	}}
	withHubCLIBackend(t, fixture, nil)
	var stdout, stderr bytes.Buffer
	if code := runHubCLI(context.Background(), []string{"status", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	var output hubStatusJSON
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if !output.Hosts[0].Load1.Available || output.Hosts[0].Load1.Value != 0 || !output.Hosts[0].LatencyMS.Available || output.Hosts[0].LatencyMS.Value != 0 {
		t.Fatalf("zero values lost: %#v", output.Hosts[0])
	}
}

func TestHubServicesJSONPreservesServiceOrderAndAvailability(t *testing.T) {
	fixture := cliFixture()
	fixture.snapshots = fixture.snapshots[:1]
	withHubCLIBackend(t, fixture, nil)
	var stdout, stderr bytes.Buffer
	if code := runHubCLI(context.Background(), []string{"services", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	var output hubServicesJSON
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if len(output.Services) != 1 || output.Services[0].ID != "api" || !output.Services[0].ActiveState.Available {
		t.Fatalf("output = %#v", output)
	}
}

func TestHubServiceFailureDoesNotFailHostStatus(t *testing.T) {
	fixture := cliFixture()
	fixture.snapshots = []hub.HostSnapshot{{
		HostID: "first", Hostname: hub.Available[string]{Value: "one", Available: true},
		Services: []hub.ServiceSnapshot{{ServiceID: "api", Unit: "api.service", Error: "service status failed"}},
	}}
	withHubCLIBackend(t, fixture, nil)
	var statusOut, statusErr bytes.Buffer
	if code := runHubCLI(context.Background(), []string{"status", "--json"}, &statusOut, &statusErr); code != 0 {
		t.Fatalf("host status code=%d stderr=%q", code, statusErr.String())
	}
	var servicesOut, servicesErr bytes.Buffer
	if code := runHubCLI(context.Background(), []string{"services", "--json"}, &servicesOut, &servicesErr); code != 1 {
		t.Fatalf("services code=%d stderr=%q", code, servicesErr.String())
	}
	if !strings.Contains(servicesOut.String(), "service status failed") {
		t.Fatalf("service error missing: %q", servicesOut.String())
	}
}

func TestHubCLIUsageAndConfigFailuresExitTwoOnStderr(t *testing.T) {
	for _, test := range []struct {
		args []string
		err  error
	}{{args: []string{"status", "--bad"}}, {args: []string{"config"}, err: errors.New("invalid inventory")}} {
		withHubCLIBackend(t, cliFixture(), test.err)
		var stdout, stderr bytes.Buffer
		if code := runHubCLI(context.Background(), test.args, &stdout, &stderr); code != 2 || stdout.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf("args=%v stdout=%q stderr=%q", test.args, stdout.String(), stderr.String())
		}
	}
}

func TestHubCommandRecognitionDoesNotClaimUpstreamCommands(t *testing.T) {
	for _, command := range []string{"config", "status", "services"} {
		if !isHubCommand(command) {
			t.Fatalf("Hub command %q not recognized", command)
		}
	}
	for _, command := range []string{"list", "session", "group", "update"} {
		if isHubCommand(command) {
			t.Fatalf("upstream command %q claimed by Hub dispatch", command)
		}
	}
}

func TestHubInvocationBoundaryRejectsEmptyAndUpstreamCommands(t *testing.T) {
	for _, args := range [][]string{nil, {}, {"list"}, {"session", "start", "worker"}, {"config"}} {
		if !hubInvocationUsesReadOnlyCLI(args) {
			t.Fatalf("args %v escaped Hub CLI boundary", args)
		}
	}
	for _, args := range [][]string{{"version"}, {"--version"}, {"-v"}, {"help"}, {"--help"}, {"-h"}} {
		if hubInvocationUsesReadOnlyCLI(args) {
			t.Fatalf("args %v did not reach read-only version/help", args)
		}
	}
}
