package hub

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	DefaultJournalLines = 200
	statusOutputLimit   = 8 * 1024
	journalOutputLimit  = 128 * 1024
	restartOutputLimit  = 8 * 1024
)

type ServiceManager interface {
	Status(context.Context, Host, Service) (ServiceSnapshot, error)
	Logs(context.Context, Host, Service) ([]byte, error)
	Restart(context.Context, Host, Service) error
}

type SystemdManager struct {
	Transport SSHExecutor
	Timeout   time.Duration
	LogLines  int
}

func NewSystemdManager(transport SSHExecutor) *SystemdManager {
	return &SystemdManager{Transport: transport, Timeout: DefaultSSHTimeout, LogLines: DefaultJournalLines}
}

func (m *SystemdManager) Status(ctx context.Context, host Host, service Service) (ServiceSnapshot, error) {
	snapshot := ServiceSnapshot{ServiceID: service.ID, Unit: service.Unit}
	if err := validateServiceOperation(m, host, service, ActionStatus); err != nil {
		return snapshot, err
	}
	result, err := m.Transport.Run(ctx, SSHRequest{
		Target: host.Target, Command: fixedRemoteCommand(systemdStatusCommand(service)), Timeout: m.timeout(),
		StdoutLimit: statusOutputLimit, StderrLimit: statusOutputLimit,
	})
	if err != nil {
		return snapshot, fmt.Errorf("systemd status for service %q: %w", service.ID, err)
	}
	parsed, err := parseSystemdStatus(result.Stdout)
	parsed.ServiceID = service.ID
	parsed.Unit = service.Unit
	if err != nil {
		return parsed, fmt.Errorf("systemd status for service %q: %w", service.ID, err)
	}
	return parsed, nil
}

func (m *SystemdManager) Logs(ctx context.Context, host Host, service Service) ([]byte, error) {
	if err := validateServiceOperation(m, host, service, ActionLogs); err != nil {
		return nil, err
	}
	lines := m.LogLines
	if lines <= 0 {
		lines = DefaultJournalLines
	}
	if lines > 1000 {
		return nil, fmt.Errorf("systemd logs for service %q: line limit exceeds 1000", service.ID)
	}
	result, err := m.Transport.Run(ctx, SSHRequest{
		Target: host.Target, Command: fixedRemoteCommand(systemdLogsCommand(service, lines)), Timeout: m.timeout(),
		StdoutLimit: journalOutputLimit, StderrLimit: statusOutputLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("systemd logs for service %q: %w", service.ID, err)
	}
	return append([]byte(nil), result.Stdout...), nil
}

func (m *SystemdManager) Restart(ctx context.Context, host Host, service Service) error {
	if err := validateServiceOperation(m, host, service, ActionRestart); err != nil {
		return err
	}
	_, err := m.Transport.Run(ctx, SSHRequest{
		Target: host.Target, Command: fixedRemoteCommand(systemdRestartCommand(service)), Timeout: m.timeout(),
		StdoutLimit: restartOutputLimit, StderrLimit: restartOutputLimit,
	})
	if err != nil {
		return fmt.Errorf("systemd restart for service %q: %w", service.ID, err)
	}
	return nil
}

func (m *SystemdManager) timeout() time.Duration {
	if m.Timeout > 0 {
		return m.Timeout
	}
	return DefaultSSHTimeout
}

func validateServiceOperation(manager *SystemdManager, host Host, service Service, action Action) error {
	if manager == nil || manager.Transport == nil {
		return fmt.Errorf("systemd %s: transport is unavailable", action)
	}
	if err := validateTarget(host.Target); err != nil {
		return fmt.Errorf("systemd %s: invalid host target", action)
	}
	if !idPattern.MatchString(service.ID) {
		return fmt.Errorf("systemd %s: invalid service id", action)
	}
	if !unitPattern.MatchString(service.Unit) {
		return fmt.Errorf("systemd %s for service %q: invalid unit", action, service.ID)
	}
	if service.Scope != ScopeSystem && service.Scope != ScopeUser {
		return fmt.Errorf("systemd %s for service %q: invalid scope", action, service.ID)
	}
	for _, allowed := range service.Actions {
		if allowed == action {
			return nil
		}
	}
	return fmt.Errorf("systemd %s is not allowed for service %q", action, service.ID)
}

func systemdStatusCommand(service Service) string {
	prefix := "systemctl"
	if service.Scope == ScopeUser {
		prefix += " --user"
	}
	return prefix + " show --no-pager --property=ActiveState --property=SubState " + service.Unit
}

func systemdLogsCommand(service Service, lines int) string {
	prefix := "journalctl"
	if service.Scope == ScopeSystem {
		prefix = "sudo -n journalctl"
	} else {
		prefix += " --user"
	}
	return fmt.Sprintf("%s --no-pager -n %d -u %s", prefix, lines, service.Unit)
}

func systemdRestartCommand(service Service) string {
	prefix := "sudo -n systemctl"
	if service.Scope == ScopeUser {
		prefix = "systemctl --user"
	}
	return prefix + " restart " + service.Unit
}

func parseSystemdStatus(data []byte) (ServiceSnapshot, error) {
	var snapshot ServiceSnapshot
	values := make(map[string]string, 2)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 1024), statusOutputLimit)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if !ok || (key != "ActiveState" && key != "SubState") {
			return snapshot, fmt.Errorf("parse systemd status: unexpected output")
		}
		if _, exists := values[key]; exists {
			return snapshot, fmt.Errorf("parse systemd status: duplicate %s", key)
		}
		if !safeDisplayValue(value) {
			return snapshot, fmt.Errorf("parse systemd status: invalid %s", key)
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return snapshot, fmt.Errorf("parse systemd status: output exceeds limit")
	}
	active, activeOK := values["ActiveState"]
	sub, subOK := values["SubState"]
	if !activeOK || !subOK {
		return snapshot, fmt.Errorf("parse systemd status: missing state")
	}
	snapshot.ActiveState = Available[string]{Value: active, Available: true}
	snapshot.SubState = Available[string]{Value: sub, Available: true}
	return snapshot, nil
}
