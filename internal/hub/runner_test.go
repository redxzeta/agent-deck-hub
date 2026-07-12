package hub

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"testing"
	"time"
)

func TestExecRunnerCapturesSeparateOutputAndExitCode(t *testing.T) {
	runner := NewExecRunner()
	result, err := runner.Run(context.Background(), helperCommand("output", 128, 128))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(result.Stdout) != "stdout" || string(result.Stderr) != "stderr" || result.ExitCode != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestExecRunnerBoundsOutput(t *testing.T) {
	runner := NewExecRunner()
	result, err := runner.Run(context.Background(), helperCommand("large", 4, 3))
	if !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("error = %v, want ErrOutputLimit", err)
	}
	if string(result.Stdout) != "xxxx" || string(result.Stderr) != "yyy" {
		t.Fatalf("bounded output = %q, %q", result.Stdout, result.Stderr)
	}
}

func TestExecRunnerHonorsTimeoutAndCancellation(t *testing.T) {
	runner := NewExecRunner()
	command := helperCommand("wait", 128, 128)
	command.Timeout = 20 * time.Millisecond
	_, err := runner.Run(context.Background(), command)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = runner.Run(ctx, helperCommand("wait", 128, 128))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestExecRunnerCommandContextIsInjectable(t *testing.T) {
	called := false
	runner := &ExecRunner{CommandContext: func(ctx context.Context, name string, args ...string) *exec.Cmd {
		called = true
		return exec.CommandContext(ctx, "sh", "-c", `printf stdout; printf stderr >&2`)
	}}
	if _, err := runner.Run(context.Background(), Command{Name: "ignored"}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("injected CommandContext was not called")
	}
}

func helperCommand(mode string, stdoutLimit, stderrLimit int) Command {
	script := map[string]string{
		"output": `printf stdout; printf stderr >&2`,
		"large":  `printf xxxxxxxx; printf yyyyyyyy >&2`,
		"wait":   `sleep 1`,
	}[mode]
	return Command{
		Name:        "sh",
		Args:        []string{"-c", script},
		Timeout:     time.Second,
		StdoutLimit: stdoutLimit,
		StderrLimit: stderrLimit,
	}
}

type recordingRunner struct {
	commands []Command
	result   CommandResult
	err      error
}

func (r *recordingRunner) Run(_ context.Context, command Command) (CommandResult, error) {
	r.commands = append(r.commands, command)
	return r.result, r.err
}

func TestSSHTransportBuildsFixedOpenSSHArgv(t *testing.T) {
	runner := &recordingRunner{result: CommandResult{Stdout: []byte("ok")}}
	transport := NewSSHTransport(runner)
	result, err := transport.Run(context.Background(), SSHRequest{
		Target:      "prod-alias",
		Command:     fixedRemoteCommand("printf fixed"),
		Timeout:     9 * time.Second,
		StdoutLimit: 42,
		StderrLimit: 21,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Stdout) != "ok" || len(runner.commands) != 1 {
		t.Fatalf("result=%#v commands=%d", result, len(runner.commands))
	}
	want := Command{
		Name:    "ssh",
		Args:    []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "--", "prod-alias", "printf fixed"},
		Timeout: 9 * time.Second, StdoutLimit: 42, StderrLimit: 21,
	}
	if !reflect.DeepEqual(runner.commands[0], want) {
		t.Fatalf("command = %#v, want %#v", runner.commands[0], want)
	}
}

func TestSSHTransportRejectsUnsafeRequestBeforeRunner(t *testing.T) {
	runner := &recordingRunner{}
	transport := NewSSHTransport(runner)
	for _, request := range []SSHRequest{
		{Target: "-oProxyCommand=evil", Command: fixedRemoteCommand("fixed")},
		{Target: "host\nother", Command: fixedRemoteCommand("fixed")},
		{Target: "host"},
	} {
		if _, err := transport.Run(context.Background(), request); err == nil {
			t.Fatalf("request %#v unexpectedly succeeded", request)
		}
	}
	if len(runner.commands) != 0 {
		t.Fatalf("runner received %d unsafe commands", len(runner.commands))
	}
}

func TestSSHTransportErrorOmitsCapturedOutput(t *testing.T) {
	runner := &recordingRunner{
		result: CommandResult{Stdout: []byte("token=secret"), Stderr: []byte("private-key=/secret")},
		err:    errors.New("exit status 255"),
	}
	transport := NewSSHTransport(runner)
	_, err := transport.Run(context.Background(), SSHRequest{Target: "host", Command: fixedRemoteCommand("fixed")})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got != `run ssh for target "host": exit status 255` {
		t.Fatalf("sanitized error = %q", got)
	}
}
