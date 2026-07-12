package hub

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

const (
	DefaultSSHTimeout        = 15 * time.Second
	DefaultSSHConnectTimeout = 5 * time.Second
)

// RemoteCommand has no exported fields or public constructor. Hub subsystems
// define fixed commands and templates beside their parsers/managers; inventory
// and external callers cannot supply arbitrary remote shell text.
type RemoteCommand struct {
	value string
}

func fixedRemoteCommand(value string) RemoteCommand { return RemoteCommand{value: value} }

type SSHRequest struct {
	Target      string
	Command     RemoteCommand
	Timeout     time.Duration
	StdoutLimit int
	StderrLimit int
}

type SSHTransport struct {
	Runner         CommandRunner
	Binary         string
	ConnectTimeout time.Duration
}

func NewSSHTransport(runner CommandRunner) *SSHTransport {
	return &SSHTransport{Runner: runner, Binary: "ssh", ConnectTimeout: DefaultSSHConnectTimeout}
}

func (t *SSHTransport) Run(ctx context.Context, request SSHRequest) (CommandResult, error) {
	if t == nil || t.Runner == nil {
		return CommandResult{}, fmt.Errorf("run ssh: command runner is nil")
	}
	if err := validateTarget(request.Target); err != nil {
		return CommandResult{}, fmt.Errorf("run ssh: target %w", err)
	}
	if request.Command.value == "" {
		return CommandResult{}, fmt.Errorf("run ssh: remote command is empty")
	}

	binary := t.Binary
	if binary == "" {
		binary = "ssh"
	}
	connectTimeout := t.ConnectTimeout
	if connectTimeout == 0 {
		connectTimeout = DefaultSSHConnectTimeout
	}
	if connectTimeout < 0 {
		return CommandResult{}, fmt.Errorf("run ssh: connect timeout must not be negative")
	}
	connectSeconds := int(connectTimeout.Round(time.Second) / time.Second)
	if connectSeconds < 1 {
		connectSeconds = 1
	}

	timeout := request.Timeout
	if timeout == 0 {
		timeout = DefaultSSHTimeout
	}
	command := Command{
		Name: binary,
		Args: []string{
			"-o", "BatchMode=yes",
			"-o", "ConnectTimeout=" + strconv.Itoa(connectSeconds),
			"--", request.Target, request.Command.value,
		},
		Timeout:     timeout,
		StdoutLimit: request.StdoutLimit,
		StderrLimit: request.StderrLimit,
	}
	result, err := t.Runner.Run(ctx, command)
	if err != nil {
		// Raw remote output is intentionally retained only in result and never
		// interpolated into errors, logs, audit records, or telemetry.
		return result, fmt.Errorf("run ssh for target %q: %w", request.Target, err)
	}
	return result, nil
}
