package whispercpp

import (
	"context"
	"os/exec"
)

// maxStderr bounds the stderr bytes kept from one command.
const maxStderr = 4096

// CommandRunner runs one external command to completion. It returns the tail
// of the command's stderr and a non-nil error on failure or cancellation.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string) (stderr []byte, err error)
}

// ExecRunner runs commands with os/exec. The child gets no stdin and its
// stdout is discarded; it is killed when ctx is done.
type ExecRunner struct{}

// Run executes name with args.
func (ExecRunner) Run(ctx context.Context, name string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr tailBuffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctxErr := ctx.Err(); ctxErr != nil && err != nil {
		return stderr.Bytes(), ctxErr
	}
	return stderr.Bytes(), err
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
