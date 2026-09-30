package context

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// DefaultPipelineVersion is the provenance pipelineVersion of emitted
// ContextEvents unless overridden.
const DefaultPipelineVersion = "poc-v1"

// EngineOption customizes an Engine.
type EngineOption func(*Engine)

// WithCorrelator replaces the default SegmentCorrelator.
func WithCorrelator(c Correlator) EngineOption {
	return func(e *Engine) {
		if c != nil {
			e.correlator = c
		}
	}
}

// WithPipelineVersion overrides DefaultPipelineVersion.
func WithPipelineVersion(version string) EngineOption {
	return func(e *Engine) { e.pipelineVersion = version }
}

// Engine turns the observations of one segment into ContextEvents:
// correlate, then reason over each group, then ground and map every
// candidate. It is all-or-nothing per segment: any failure returns no events.
type Engine struct {
	provider        string
	reasoner        Reasoner
	correlator      Correlator
	pipelineVersion string
}

// NewEngine returns an Engine for the reasoner registered as provider.
func NewEngine(provider string, reasoner Reasoner, opts ...EngineOption) (*Engine, error) {
	if strings.TrimSpace(provider) == "" {
		return nil, errors.New("context: blank reasoning provider")
	}
	if reasoner == nil {
		return nil, errors.New("context: nil reasoner")
	}
	e := &Engine{provider: provider, reasoner: reasoner, correlator: SegmentCorrelator{}, pipelineVersion: DefaultPipelineVersion}
	for _, opt := range opts {
		opt(e)
	}
	if strings.TrimSpace(e.pipelineVersion) == "" {
		return nil, errors.New("context: blank pipeline version")
	}
	return e, nil
}

// Provider returns the registered reasoner name the engine was built for.
func (e *Engine) Provider() string { return e.provider }

// Process correlates in, reasons over every resulting group and returns the
// mapped ContextEvents in group and candidate order. A segment without usable
// evidence yields no events and no reasoner call.
func (e *Engine) Process(ctx context.Context, in CorrelationInput) ([]contracts.ContextEventV1, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	groups, err := e.correlator.Correlate(in)
	if err != nil {
		var staged *Error
		if errors.As(err, &staged) {
			return nil, err
		}
		return nil, SegmentError(StageCorrelate, in.Segment, "", err)
	}
	var events []contracts.ContextEventV1
	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		candidates, err := e.reasoner.Reason(ctx, group.Clone())
		if err != nil {
			return nil, SegmentError(StageReason, in.Segment, e.provider, err)
		}
		for i, c := range candidates {
			if c.Reasoning.Provider != e.provider {
				return nil, SegmentError(StageReason, in.Segment, e.provider, fmt.Errorf(
					"candidate %d: reasoning provider %q is not the configured provider %q", i+1, c.Reasoning.Provider, e.provider))
			}
			if err := Ground(group, c); err != nil {
				return nil, SegmentError(StageReason, in.Segment, e.provider, fmt.Errorf("candidate %d: %w", i+1, err))
			}
		}
		for _, c := range candidates {
			event, err := NewContextEvent(group, c, len(events)+1, e.pipelineVersion)
			if err != nil {
				return nil, SegmentError(StageMapContext, in.Segment, e.provider, err)
			}
			events = append(events, event)
		}
	}
	return events, nil
}
