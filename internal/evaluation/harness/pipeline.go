package harness

import (
	"context"

	"github.com/ISR-Consulting/context-video/internal/evaluation/config"
	"github.com/ISR-Consulting/context-video/internal/evaluation/dataset"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// PipelineInput is one validated test case offered to a pipeline.
type PipelineInput struct {
	Config   config.Config
	TestCase dataset.LoadedTestCase
}

// PipelineOutput is the contract-level output of processing one test case.
// Every value is validated by the runner before it is accepted.
type PipelineOutput struct {
	AudioObservations  []contracts.AudioObservation
	VisualObservations []contracts.VisualObservation
	ContextEvents      []contracts.ContextEventV1
}

// Pipeline processes one test case. Implementations must not create
// ExperimentResult values, persist files or iterate the dataset.
type Pipeline interface {
	Process(ctx context.Context, input PipelineInput) (PipelineOutput, error)
}

// PipelineFunc adapts a function to Pipeline.
type PipelineFunc func(ctx context.Context, input PipelineInput) (PipelineOutput, error)

// Process calls f.
func (f PipelineFunc) Process(ctx context.Context, input PipelineInput) (PipelineOutput, error) {
	return f(ctx, input)
}

// ValidationOnlyPipeline returns empty output for every test case. It exercises
// configuration, dataset loading, orchestration and persistence without any
// provider. It is an infrastructure smoke mode, not an E01–E05 benchmark.
type ValidationOnlyPipeline struct{}

// Process returns empty output unless ctx is already done.
func (ValidationOnlyPipeline) Process(ctx context.Context, _ PipelineInput) (PipelineOutput, error) {
	if err := ctx.Err(); err != nil {
		return PipelineOutput{}, err
	}
	return PipelineOutput{}, nil
}
