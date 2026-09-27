package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/ISR-Consulting/context-video/internal/evaluation/config"
	"github.com/ISR-Consulting/context-video/internal/evaluation/dataset"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// LoadDataset loads and validates a golden dataset through the M02 loader.
// manifestPath is relative to datasetFS. Failures are qualified with
// StageSpecs or StageDataset and wrap the M02 error.
func LoadDataset(datasetFS, specsFS fs.FS, manifestPath string) (dataset.Dataset, error) {
	loader, err := dataset.NewLoader(datasetFS, specsFS)
	if err != nil {
		return dataset.Dataset{}, &Error{Stage: StageSpecs, Err: err}
	}
	loaded, err := loader.LoadDataset(manifestPath)
	if err != nil {
		return dataset.Dataset{}, &Error{Stage: StageDataset, Path: manifestPath, Err: err}
	}
	return loaded, nil
}

// RunInput is a validated configuration and dataset for one run.
type RunInput struct {
	Config       config.Config
	Dataset      dataset.Dataset
	ManifestPath string
}

// CaseOutput is the validated pipeline output for one test case.
type CaseOutput struct {
	TestCaseID string
	Output     PipelineOutput
}

// RunMetadata is in-memory execution metadata. It has no ExperimentResult
// field and is not persisted.
type RunMetadata struct {
	StartedAt          time.Time
	FinishedAt         time.Time
	Elapsed            time.Duration
	ProcessedTestCases int
	AudioObservations  int
	VisualObservations int
	ContextEvents      int
}

// RunOutcome is the result of a successful run. Cases are in manifest order.
type RunOutcome struct {
	Result   contracts.ExperimentResult
	Dataset  config.DatasetIdentity
	Metadata RunMetadata
	Cases    []CaseOutput
}

// Option configures a Runner.
type Option func(*Runner)

// WithClock replaces time.Now as the runner clock.
func WithClock(now func() time.Time) Option {
	return func(r *Runner) {
		if now != nil {
			r.now = now
		}
	}
}

// Runner executes a pipeline over a dataset sequentially in manifest order.
type Runner struct {
	validator *contracts.Validator
	pipeline  Pipeline
	now       func() time.Time
}

// NewRunner returns a Runner that validates pipeline output with validator.
func NewRunner(validator *contracts.Validator, pipeline Pipeline, opts ...Option) (*Runner, error) {
	if validator == nil {
		return nil, errors.New("harness: nil validator")
	}
	if pipeline == nil {
		return nil, errors.New("harness: nil pipeline")
	}
	r := &Runner{validator: validator, pipeline: pipeline, now: time.Now}
	for _, opt := range opts {
		opt(r)
	}
	return r, nil
}

// Run processes every test case once, in order, and builds a validated
// ExperimentResult. It stops at the first failing test case and returns no
// partial outcome.
func (r *Runner) Run(ctx context.Context, in RunInput) (RunOutcome, error) {
	if err := ctx.Err(); err != nil {
		return RunOutcome{}, &Error{Stage: StageCancelled, Err: err}
	}
	start := r.now()
	cases := make([]CaseOutput, 0, len(in.Dataset.TestCases))
	var meta RunMetadata
	for _, testCase := range in.Dataset.TestCases {
		id := testCase.TestCase.TestCaseID
		if err := ctx.Err(); err != nil {
			return RunOutcome{}, &Error{Stage: StageCancelled, TestCaseID: id, Err: err}
		}
		output, err := r.pipeline.Process(ctx, PipelineInput{Config: in.Config, TestCase: testCase})
		if err != nil {
			stage := StagePipeline
			if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
				stage = StageCancelled
			}
			return RunOutcome{}, &Error{Stage: stage, TestCaseID: id, Err: err}
		}
		if err := r.validateOutput(in.Config, testCase.TestCase, output); err != nil {
			return RunOutcome{}, err
		}
		cases = append(cases, CaseOutput{TestCaseID: id, Output: output})
		meta.ProcessedTestCases++
		meta.AudioObservations += len(output.AudioObservations)
		meta.VisualObservations += len(output.VisualObservations)
		meta.ContextEvents += len(output.ContextEvents)
	}
	end := r.now()
	meta.StartedAt = start.UTC()
	meta.FinishedAt = end.UTC()
	meta.Elapsed = end.Sub(start)

	identity := config.DatasetIdentity{
		DatasetID:      in.Dataset.Manifest.DatasetID,
		DatasetVersion: in.Dataset.Manifest.DatasetVersion,
		ManifestPath:   in.ManifestPath,
	}
	result := NewResult(in.Config.ExperimentID, meta.StartedAt, in.Config.Snapshot(identity))
	if err := ValidateResult(r.validator, result); err != nil {
		return RunOutcome{}, err
	}
	return RunOutcome{Result: result, Dataset: identity, Metadata: meta, Cases: cases}, nil
}

func (r *Runner) validateOutput(cfg config.Config, testCase dataset.TestCase, output PipelineOutput) error {
	id := testCase.TestCaseID
	check := func(kind contracts.Kind, path string, content contracts.ContentRef, value any) error {
		data, err := json.Marshal(value)
		if err != nil {
			return &Error{Stage: StageOutputSchema, TestCaseID: id, Path: path, Err: err}
		}
		if err := r.validator.ValidateJSON(kind, data); err != nil {
			return &Error{Stage: StageOutputSchema, TestCaseID: id, Path: path, Err: err}
		}
		if err := contracts.Validate(value); err != nil {
			return &Error{Stage: StageOutputDomain, TestCaseID: id, Path: path, Err: err}
		}
		if content != testCase.Content {
			return &Error{Stage: StageOutputContent, TestCaseID: id, Path: path, Err: fmt.Errorf(
				"content %s/%s does not match test case content %s/%s",
				content.ContentID, content.ContentType, testCase.Content.ContentID, testCase.Content.ContentType)}
		}
		return nil
	}

	for i, obs := range output.AudioObservations {
		if err := check(contracts.KindAudioObservation, fmt.Sprintf("audioObservations[%d]", i), obs.Content, obs); err != nil {
			return err
		}
	}
	for i, obs := range output.VisualObservations {
		if err := check(contracts.KindVisualObservation, fmt.Sprintf("visualObservations[%d]", i), obs.Content, obs); err != nil {
			return err
		}
	}
	for i, event := range output.ContextEvents {
		path := fmt.Sprintf("contextEvents[%d]", i)
		if err := check(contracts.KindContextEventV1, path, event.Content, event); err != nil {
			return err
		}
		if event.SchemaVersion != cfg.ContextEventSchemaVersion {
			return &Error{Stage: StageOutputSchema, TestCaseID: id, Path: path + ".schemaVersion", Err: fmt.Errorf(
				"schemaVersion %q does not match configured %q", event.SchemaVersion, cfg.ContextEventSchemaVersion)}
		}
	}

	catalog, err := contracts.NewCatalog(output.AudioObservations, output.VisualObservations)
	if err != nil {
		return &Error{Stage: StageOutputDomain, TestCaseID: id, Path: "observations", Err: err}
	}
	for i, event := range output.ContextEvents {
		if err := event.ValidateEvidence(catalog); err != nil {
			return &Error{Stage: StageOutputEvidence, TestCaseID: id, Path: fmt.Sprintf("contextEvents[%d]", i), Err: err}
		}
	}
	return nil
}
