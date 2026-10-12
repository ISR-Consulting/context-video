package openai

import (
	"context"
	"os/exec"
)

const maxStderr = 4096

// CommandRunner runs one external command to completion.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string) (stderr []byte, err error)
}

// ExecRunner runs commands with os/exec.
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
