package llamacpp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	contextcore "github.com/ISR-Consulting/context-video/internal/context"
)

// ProviderName is the registry name and provenance fusionProvider of this
// adapter.
const ProviderName = "llama-cpp"

// Step identifies which part of a reasoning call failed.
type Step string

const (
	StepWorkspace Step = "workspace"
	StepReason    Step = "reason"
	StepParse     Step = "parse"
)

// Error is a step-qualified adapter failure. Stderr holds the tail of the
// failing command's stderr and Output an excerpt of an unparseable model
// answer, when there are any.
type Error struct {
	Step      Step
	SegmentID string
	Stderr    string
	Output    string
	Err       error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	parts := []string{ProviderName + " " + string(e.Step)}
	if e.SegmentID != "" {
		parts = append(parts, "segment "+e.SegmentID)
	}
	if e.Err != nil {
		parts = append(parts, e.Err.Error())
	}
	msg := strings.Join(parts, ": ")
	if e.Output != "" {
		msg += " (output: " + e.Output + ")"
	}
	if e.Stderr != "" {
		msg += " (stderr: " + e.Stderr + ")"
	}
	return msg
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Adapter reasons over evidence groups with a local llama.cpp text model.
type Adapter struct {
	cfg    Config
	runner CommandRunner
	tmpDir string
}

// AdapterOption customizes an Adapter.
type AdapterOption func(*Adapter)

// WithRunner replaces the os/exec command runner.
func WithRunner(r CommandRunner) AdapterOption {
	return func(a *Adapter) {
		if r != nil {
			a.runner = r
		}
	}
}

// WithTempDir sets the parent directory for per-request scratch directories.
// The default is os.TempDir.
func WithTempDir(dir string) AdapterOption {
	return func(a *Adapter) { a.tmpDir = dir }
}

// New returns an Adapter for cfg. Model is required; other fields default to
// llama-completion and 1024 answer tokens.
func New(cfg Config, opts ...AdapterOption) (*Adapter, error) {
	cfg, err := cfg.withDefaults()
	if err != nil {
		return nil, err
	}
	a := &Adapter{cfg: cfg, runner: ExecRunner{}}
	for _, opt := range opts {
		opt(a)
	}
	return a, nil
}

// NewFromOptions is the contextcore.Factory for ProviderName. Supported keys
// are model (required), binary, threads, gpu-layers, max-tokens and ctx-size.
func NewFromOptions(opts contextcore.Options) (contextcore.Reasoner, error) {
	cfg, err := configFromOptions(opts)
	if err != nil {
		return nil, err
	}
	return New(cfg)
}

// Model returns the descriptive model label recorded in provenance: the model
// file name without its directory.
func (a *Adapter) Model() string {
	return filepath.Base(a.cfg.Model)
}

// Reason runs one single-turn llama.cpp chat over group and parses the
// grammar-constrained answer into candidates. A group without evidence runs
// nothing and yields no candidates.
func (a *Adapter) Reason(ctx context.Context, group contextcore.EvidenceGroup) ([]contextcore.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(group.Evidence) == 0 {
		return nil, nil
	}
	fail := func(step Step, stderr []byte, err error) ([]contextcore.Candidate, error) {
		return nil, &Error{Step: step, SegmentID: group.SegmentID, Stderr: strings.TrimSpace(string(stderr)), Err: err}
	}
	user, err := UserPrompt(group)
	if err != nil {
		return fail(StepWorkspace, nil, err)
	}
	schema, err := ResponseSchema(group)
	if err != nil {
		return fail(StepWorkspace, nil, err)
	}

	dir, err := os.MkdirTemp(a.tmpDir, "llamacpp-*")
	if err != nil {
		return fail(StepWorkspace, nil, err)
	}
	defer os.RemoveAll(dir)
	files := map[string][]byte{
		"system.txt":  []byte(SystemPrompt),
		"user.txt":    user,
		"schema.json": schema,
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			return fail(StepWorkspace, nil, err)
		}
	}

	stdout, stderr, err := a.runner.Run(ctx, a.cfg.Binary, a.args(dir))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return fail(StepReason, stderr, fmt.Errorf("%s: %w", a.cfg.Binary, err))
	}
	candidates, err := parseAnswer(stdout, contextcore.ReasoningMetadata{
		Provider:      ProviderName,
		Model:         a.Model(),
		PromptVersion: PromptVersion,
	}, sources(group))
	if err != nil {
		return nil, &Error{Step: StepParse, SegmentID: group.SegmentID, Output: excerpt(stdout), Err: err}
	}
	return candidates, nil
}

// args runs one chat turn: the model's chat template wraps the system prompt
// and the evidence message, --json-schema-file constrains generation, and
// --offline plus the absence of any -hf flag keep llama.cpp off the network.
func (a *Adapter) args(dir string) []string {
	args := []string{
		"-m", a.cfg.Model,
		"-cnv", "-st",
		"-sysf", filepath.Join(dir, "system.txt"),
		"-f", filepath.Join(dir, "user.txt"),
		"--json-schema-file", filepath.Join(dir, "schema.json"),
		"--temp", "0",
		"--seed", "0",
		"-n", strconv.Itoa(a.cfg.MaxTokens),
		"--no-display-prompt",
		"--simple-io",
		"--no-warmup",
		"--offline",
	}
	if a.cfg.Threads > 0 {
		args = append(args, "-t", strconv.Itoa(a.cfg.Threads))
	}
	if a.cfg.GPULayers != nil {
		args = append(args, "-ngl", strconv.Itoa(*a.cfg.GPULayers))
	}
	if a.cfg.CtxSize > 0 {
		args = append(args, "-c", strconv.Itoa(a.cfg.CtxSize))
	}
	return args
}

var _ contextcore.Reasoner = (*Adapter)(nil)
