package hub

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type systemdTransport struct {
	requests []SSHRequest
	result   CommandResult
	err      error
}

func (t *systemdTransport) Run(_ context.Context, request SSHRequest) (CommandResult, error) {
	t.requests = append(t.requests, request)
	return t.result, t.err
}

func systemdFixture(scope Scope, actions ...Action) (Host, Service) {
	return Host{ID: "host", Target: "prod-alias"}, Service{ID: "api", Unit: "example-api@blue.service", Scope: scope, Actions: actions}
}

func TestSystemdStatusTemplatesAndParsing(t *testing.T) {
	for _, test := range []struct {
		scope Scope
		want  string
	}{
		{ScopeSystem, "systemctl show --no-pager --property=ActiveState --property=SubState example-api@blue.service"},
		{ScopeUser, "systemctl --user show --no-pager --property=ActiveState --property=SubState example-api@blue.service"},
	} {
		transport := &systemdTransport{result: CommandResult{Stdout: []byte("ActiveState=active\nSubState=running\n")}}
		manager := NewSystemdManager(transport)
		host, service := systemdFixture(test.scope, ActionStatus)
		snapshot, err := manager.Status(context.Background(), host, service)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.ActiveState.Value != "active" || snapshot.SubState.Value != "running" || snapshot.ServiceID != "api" {
			t.Fatalf("snapshot = %#v", snapshot)
		}
		request := transport.requests[0]
		if request.Command.value != test.want || request.StdoutLimit != statusOutputLimit || request.StderrLimit != statusOutputLimit {
			t.Fatalf("request = %#v", request)
		}
	}
}

func TestSystemdLogsAndRestartScopeSelection(t *testing.T) {
	for _, test := range []struct {
		scope       Scope
		wantLogs    string
		wantRestart string
	}{
		{ScopeSystem, "sudo -n journalctl --no-pager -n 200 -u example-api@blue.service", "sudo -n systemctl restart example-api@blue.service"},
		{ScopeUser, "journalctl --user --no-pager -n 200 --user-unit=example-api@blue.service", "systemctl --user restart example-api@blue.service"},
	} {
		transport := &systemdTransport{result: CommandResult{Stdout: []byte("bounded logs")}}
		manager := NewSystemdManager(transport)
		host, service := systemdFixture(test.scope, ActionLogs, ActionRestart)
		logs, err := manager.Logs(context.Background(), host, service)
		if err != nil || string(logs) != "bounded logs" {
			t.Fatalf("logs=%q err=%v", logs, err)
		}
		if err := manager.Restart(context.Background(), host, service); err != nil {
			t.Fatal(err)
		}
		if transport.requests[0].Command.value != test.wantLogs || transport.requests[0].StdoutLimit != journalOutputLimit {
			t.Fatalf("logs request = %#v", transport.requests[0])
		}
		if transport.requests[1].Command.value != test.wantRestart || transport.requests[1].StdoutLimit != restartOutputLimit {
			t.Fatalf("restart request = %#v", transport.requests[1])
		}
	}
}

func TestSystemdRejectsBeforeTransport(t *testing.T) {
	tests := []Service{
		{ID: "api", Unit: "api.service", Scope: ScopeSystem, Actions: []Action{ActionLogs}},
		{ID: "api", Unit: "bad unit.service", Scope: ScopeSystem, Actions: []Action{ActionStatus}},
		{ID: "api", Unit: "api.service", Scope: "root", Actions: []Action{ActionStatus}},
	}
	for _, service := range tests {
		transport := &systemdTransport{}
		manager := NewSystemdManager(transport)
		if _, err := manager.Status(context.Background(), Host{ID: "host", Target: "alias"}, service); err == nil {
			t.Fatalf("service %#v unexpectedly accepted", service)
		}
		if len(transport.requests) != 0 {
			t.Fatal("transport called for invalid or disallowed operation")
		}
	}
}

func TestParseSystemdStatusRejectsUnsafeOrMalformedState(t *testing.T) {
	for _, output := range []string{
		"ActiveState=active\n", "ActiveState=active\nActiveState=failed\nSubState=running\n",
		"ActiveState=active\x1b\nSubState=running\n", "Unknown=value\nActiveState=active\nSubState=running\n",
	} {
		if _, err := parseSystemdStatus([]byte(output)); err == nil {
			t.Fatalf("output %q unexpectedly accepted", output)
		}
	}
}

func TestSystemdErrorsDoNotIncludeCapturedOutput(t *testing.T) {
	transport := &systemdTransport{result: CommandResult{Stdout: []byte("token=secret"), Stderr: []byte("key=/private")}, err: errors.New("ssh failed")}
	manager := NewSystemdManager(transport)
	host, service := systemdFixture(ScopeSystem, ActionStatus)
	_, err := manager.Status(context.Background(), host, service)
	if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private") {
		t.Fatalf("unsanitized error = %v", err)
	}
}

func TestSystemdLogLineLimitRejectedBeforeTransport(t *testing.T) {
	transport := &systemdTransport{}
	manager := NewSystemdManager(transport)
	manager.LogLines = 1001
	host, service := systemdFixture(ScopeSystem, ActionLogs)
	if _, err := manager.Logs(context.Background(), host, service); err == nil || len(transport.requests) != 0 {
		t.Fatalf("err=%v requests=%d", err, len(transport.requests))
	}
}
