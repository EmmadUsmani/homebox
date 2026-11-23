// Package execx runs external commands behind a small interface, so code that
// shells out (limactl, ssh, sandbox-exec) can be unit-tested with a fake.
package execx

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Cmd describes one command to run.
type Cmd struct {
	Name  string
	Args  []string
	Env   []string // added to the current environment
	Dir   string
	Stdin io.Reader
	// Stdout and Stderr, when set, stream output to the caller.
	Stdout io.Writer
	Stderr io.Writer
}

// String renders the command for logs and error messages.
func (c Cmd) String() string {
	return strings.Join(append([]string{c.Name}, c.Args...), " ")
}

// Result contains stdout when it was not redirected and a bounded stderr tail.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Runner runs commands.
type Runner interface {
	// Run waits for the command. A non-zero exit is returned as *ExitError.
	Run(ctx context.Context, cmd Cmd) (Result, error)
}

// ExitError reports a command that ran but exited non-zero.
type ExitError struct {
	Cmd      string
	ExitCode int
	Stderr   string
}

func (e *ExitError) Error() string {
	msg := fmt.Sprintf("%s: exit status %d", e.Cmd, e.ExitCode)
	if s := strings.TrimSpace(e.Stderr); s != "" {
		msg += ": " + s
	}
	return msg
}

// OS runs commands on the real system.
type OS struct{}

func (OS) Run(ctx context.Context, c Cmd) (Result, error) {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Dir = c.Dir
	cmd.Stdin = c.Stdin
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}

	var stdout bytes.Buffer
	var stderr stderrTail
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if c.Stdout != nil {
		cmd.Stdout = c.Stdout
	}
	if c.Stderr != nil {
		cmd.Stderr = io.MultiWriter(&stderr, c.Stderr)
	}

	err := cmd.Run()
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: cmd.ProcessState.ExitCode()}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return res, &ExitError{Cmd: c.String(), ExitCode: exitErr.ExitCode(), Stderr: string(res.Stderr)}
	}
	if err != nil {
		return res, fmt.Errorf("%s: %w", c.String(), err)
	}
	return res, nil
}

const stderrTailLimit = 8 << 10

type stderrTail struct {
	data [stderrTailLimit]byte
	size int
	next int
}

func (t *stderrTail) Write(data []byte) (int, error) {
	written := len(data)
	if len(data) >= len(t.data) {
		copy(t.data[:], data[len(data)-len(t.data):])
		t.size, t.next = len(t.data), 0
		return written, nil
	}
	first := copy(t.data[t.next:], data)
	copy(t.data[:], data[first:])
	t.next = (t.next + written) % len(t.data)
	t.size = min(len(t.data), t.size+written)
	return written, nil
}

func (t *stderrTail) Bytes() []byte {
	if t.size == 0 {
		return nil
	}
	if t.size < len(t.data) {
		return t.data[:t.size]
	}
	return append(append(make([]byte, 0, t.size), t.data[t.next:]...), t.data[:t.next]...)
}
