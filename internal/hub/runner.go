package hub

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

const (
	DefaultCommandTimeout = 15 * time.Second
	DefaultOutputLimit    = 256 * 1024
)

var ErrOutputLimit = errors.New("command output limit exceeded")

// Command is a fully formed local process invocation. Callers are responsible
// for constructing Name and Args from fixed program templates, never from an
// operator-provided command string.
type Command struct {
	Name        string
	Args        []string
	Timeout     time.Duration
	StdoutLimit int
	StderrLimit int
}

type CommandResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

type CommandRunner interface {
	Run(context.Context, Command) (CommandResult, error)
}

type commandContextFunc func(context.Context, string, ...string) *exec.Cmd

// ExecRunner runs commands through exec.CommandContext. CommandContext is
// injectable so unit tests can verify process behavior without real SSH.
type ExecRunner struct {
	CommandContext commandContextFunc
}

func NewExecRunner() *ExecRunner {
	return &ExecRunner{CommandContext: exec.CommandContext}
}

func (r *ExecRunner) Run(ctx context.Context, command Command) (CommandResult, error) {
	if ctx == nil {
		return CommandResult{}, fmt.Errorf("run command: context is nil")
	}
	if command.Name == "" {
		return CommandResult{}, fmt.Errorf("run command: name is empty")
	}
	if command.Timeout < 0 || command.StdoutLimit < 0 || command.StderrLimit < 0 {
		return CommandResult{}, fmt.Errorf("run command: timeout and output limits must not be negative")
	}

	timeout := command.Timeout
	if timeout == 0 {
		timeout = DefaultCommandTimeout
	}
	stdoutLimit := command.StdoutLimit
	if stdoutLimit == 0 {
		stdoutLimit = DefaultOutputLimit
	}
	stderrLimit := command.StderrLimit
	if stderrLimit == 0 {
		stderrLimit = DefaultOutputLimit
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	commandContext := r.CommandContext
	if commandContext == nil {
		commandContext = exec.CommandContext
	}
	cmd := commandContext(runCtx, command.Name, command.Args...)
	stdout := newBoundedBuffer(stdoutLimit)
	stderr := newBoundedBuffer(stderrLimit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	result := CommandResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: exitCode(err)}
	if stdout.Exceeded() {
		return result, fmt.Errorf("run command: stdout: %w", ErrOutputLimit)
	}
	if stderr.Exceeded() {
		return result, fmt.Errorf("run command: stderr: %w", ErrOutputLimit)
	}
	if err != nil {
		if runCtx.Err() != nil {
			return result, fmt.Errorf("run command: %w", runCtx.Err())
		}
		return result, fmt.Errorf("run command: %w", err)
	}
	return result, nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

type boundedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func newBoundedBuffer(limit int) *boundedBuffer { return &boundedBuffer{limit: limit} }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.exceeded = true
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = b.buffer.Write(p[:remaining])
		b.exceeded = true
		return len(p), nil
	}
	return b.buffer.Write(p)
}

func (b *boundedBuffer) Bytes() []byte {
	return append([]byte(nil), b.buffer.Bytes()...)
}

func (b *boundedBuffer) Exceeded() bool { return b.exceeded }
