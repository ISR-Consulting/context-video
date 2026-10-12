package openai

import (
	"context"
	"net/http"
	"strings"

	contextcore "github.com/ISR-Consulting/context-video/internal/context"
)

// ProviderName is the registry name and provenance fusionProvider of this adapter.
const ProviderName = "openai-reasoner"

// Step identifies which part of a reasoning call failed.
type Step string

const (
	StepWorkspace Step = "workspace"
	StepReason    Step = "reason"
	StepParse     Step = "parse"
)

// Error is a step-qualified adapter failure.
type Error struct {
	Step      Step
	SegmentID string
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
	return msg
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Adapter reasons over evidence groups with OpenAI Chat Completions.
type Adapter struct {
	cfg    Config
	client *http.Client
}

// AdapterOption customizes an Adapter.
type AdapterOption func(*Adapter)

// WithHTTPClient replaces the HTTP client used for OpenAI requests.
func WithHTTPClient(c *http.Client) AdapterOption {
	return func(a *Adapter) {
		if c != nil {
			a.client = c
		}
	}
}

// New returns an Adapter for cfg.
func New(cfg Config, opts ...AdapterOption) (*Adapter, error) {
	cfg, err := cfg.withDefaults()
	if err != nil {
		return nil, err
	}
	a := &Adapter{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.Timeout},
	}
	for _, opt := range opts {
		opt(a)
	}
	return a, nil
}

// NewFromOptions is the contextcore.Factory for ProviderName.
func NewFromOptions(opts contextcore.Options) (contextcore.Reasoner, error) {
	cfg, err := configFromOptions(opts)
	if err != nil {
		return nil, err
	}
	return New(cfg)
}

// Model returns the descriptive model label recorded in provenance.
func (a *Adapter) Model() string {
	return a.cfg.Model
}

// Reason runs one Chat Completions turn over group and parses candidates.
func (a *Adapter) Reason(ctx context.Context, group contextcore.EvidenceGroup) ([]contextcore.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(group.Evidence) == 0 {
		return nil, nil
	}
	fail := func(step Step, err error) ([]contextcore.Candidate, error) {
		return nil, &Error{Step: step, SegmentID: group.SegmentID, Err: err}
	}
	user, err := UserPrompt(group)
	if err != nil {
		return fail(StepWorkspace, err)
	}
	schema, err := ResponseSchema(group)
	if err != nil {
		return fail(StepWorkspace, err)
	}
	content, err := a.complete(ctx, string(user), schema)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return fail(StepReason, err)
	}
	candidates, err := parseAnswer(content, contextcore.ReasoningMetadata{
		Provider:      ProviderName,
		Model:         a.Model(),
		PromptVersion: PromptVersion,
	}, sources(group))
	if err != nil {
		return nil, &Error{Step: StepParse, SegmentID: group.SegmentID, Output: excerpt(content), Err: err}
	}
	return candidates, nil
}

var _ contextcore.Reasoner = (*Adapter)(nil)
