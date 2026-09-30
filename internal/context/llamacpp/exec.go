package llamacpp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
)

const (
	// maxStderr bounds the stderr bytes kept from one command.
	maxStderr = 4096
	// MaxStdout bounds the stdout bytes accepted from one command. Larger
	// output fails with ErrStdoutTooLarge instead of being truncated.
	MaxStdout = 1 << 20
)

// ErrStdoutTooLarge reports a command whose stdout exceeded MaxStdout.
var ErrStdoutTooLarge = errors.New("command stdout exceeds limit")

// CommandRunner runs one external command to completion. It returns the
// command's stdout, the tail of its stderr and a non-nil error on failure or
// cancellation.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string) (stdout, stderr []byte, err error)
}

// ExecRunner runs commands with os/exec. The child gets no stdin, inherits the
// parent environment minus LLAMA_ARG_* variables (which llama.cpp would treat
// as extra flags, including model downloads) and is killed when ctx is done.
type ExecRunner struct{}

// Run executes name with args.
func (ExecRunner) Run(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = childEnv(os.Environ())
	var stdout limitBuffer
	var stderr tailBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctxErr := ctx.Err(); ctxErr != nil && err != nil {
		return nil, stderr.Bytes(), ctxErr
	}
	if stdout.overflow {
		return nil, stderr.Bytes(), ErrStdoutTooLarge
	}
	return stdout.data, stderr.Bytes(), err
}

func childEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, "LLAMA_ARG_") {
			out = append(out, kv)
		}
	}
	return out
}

// limitBuffer keeps up to MaxStdout bytes and records overflow.
type limitBuffer struct {
	data     []byte
	overflow bool
}

func (b *limitBuffer) Write(p []byte) (int, error) {
	if room := MaxStdout - len(b.data); len(p) > room {
		b.data = append(b.data, p[:max(room, 0)]...)
		b.overflow = true
		return len(p), nil
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

// tailBuffer keeps the last maxStderr bytes written to it.
type tailBuffer struct {
	data []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	if extra := len(b.data) - maxStderr; extra > 0 {
		b.data = append(b.data[:0], b.data[extra:]...)
	}
	return len(p), nil
}

func (b *tailBuffer) Bytes() []byte { return b.data }
