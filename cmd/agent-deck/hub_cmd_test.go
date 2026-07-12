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
