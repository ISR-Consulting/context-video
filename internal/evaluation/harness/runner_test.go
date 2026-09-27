package harness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ISR-Consulting/context-video/internal/evaluation/config"
	"github.com/ISR-Consulting/context-video/internal/evaluation/dataset"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

var (
	specsRoot       = filepath.Join("..", "..", "..", "specs")
	datasetRoot     = filepath.Join("..", "dataset", "testdata", "valid")
	invalidDataRoot = filepath.Join("..", "dataset", "testdata", "invalid")
	configPath      = filepath.Join("..", "..", "..", "configs", "experiments", "multimodal-5s.yaml")
)

const fixtureManifest = "manifests/poc-golden-v1.0.json"

var fixedStart = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

func newValidator(t *testing.T) *contracts.Validator {
	t.Helper()
	v, err := contracts.NewValidator(os.DirFS(specsRoot))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func loadConfig(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func loadFixture(t *testing.T) dataset.Dataset {
	t.Helper()
	ds, err := LoadDataset(os.DirFS(datasetRoot), os.DirFS(specsRoot), fixtureManifest)
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

func runInput(t *testing.T) RunInput {
	t.Helper()
	return RunInput{Config: loadConfig(t), Dataset: loadFixture(t), ManifestPath: fixtureManifest}
}

// steppingClock returns start, then start+step, and so on.
func steppingClock(start time.Time, step time.Duration) func() time.Time {
	next := start
	return func() time.Time {
		now := next
		next = next.Add(step)
		return now
	}
}

func newRunner(t *testing.T, pipeline Pipeline) *Runner {
	t.Helper()
	r, err := NewRunner(newValidator(t), pipeline, WithClock(steppingClock(fixedStart, 1500*time.Millisecond)))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func strPtr(v string) *string { return &v }

// evidenceBacked returns valid observations and one ContextEvent citing both
// for the given test case content.
func evidenceBacked(content contracts.ContentRef) PipelineOutput {
	window := contracts.TimeWindow{StartMs: 0, EndMs: 5000}
	return PipelineOutput{
		AudioObservations: []contracts.AudioObservation{{
			ObservationID: "aud-01",
			Content:       content,
			Window:        window,
			Transcript:    contracts.Transcript{Text: "fixture transcript"},
			Provenance:    contracts.ObservationProvenance{Provider: "fake", PipelineVersion: "test"},
		}},
		VisualObservations: []contracts.VisualObservation{{
			ObservationID: "vis-01",
			Content:       content,
			Window:        window,
			Frames: []contracts.VisualFrame{{
				TimestampMs: 2500,
				Observations: []contracts.VisualDetection{{
					Type: contracts.VisualDetectionObject, Value: "football_jersey", Confidence: 0.9,
				}},
			}},
			Provenance: contracts.ObservationProvenance{Provider: "fake", PipelineVersion: "test"},
		}},
		ContextEvents: []contracts.ContextEventV1{{
			EventID:       "evt-01",
			SchemaVersion: contracts.ContextEventSchemaVersion,
			Content:       content,
			Window:        window,
			Context: contracts.ContextBody{
				Entities: []contracts.Entity{},
				Topics:   []contracts.SemanticValue{{Value: "football", Confidence: 0.8}},
				Objects:  []contracts.SemanticValue{},
				Brands:   []contracts.SemanticValue{},
			},
			Confidence: 0.8,
			Evidence: contracts.Evidence{
				Audio:  []contracts.AudioEvidence{{ObservationID: "aud-01", StartMs: 0, EndMs: 5000, Text: "fixture transcript"}},
				Visual: []contracts.VisualEvidence{{ObservationID: "vis-01", TimestampMs: 2500, Description: "jersey"}},
			},
			Provenance: contracts.ContextProvenance{PipelineVersion: "test", FusionProvider: strPtr("fake")},
		}},
	}
}

func assertHarnessError(t *testing.T, err error, stage Stage, testCaseID string) *Error {
	t.Helper()
	var harnessErr *Error
	if !errors.As(err, &harnessErr) {
		t.Fatalf("expected *harness.Error, got %T: %v", err, err)
	}
	if harnessErr.Stage != stage {
		t.Fatalf("stage: got %q want %q (%v)", harnessErr.Stage, stage, err)
	}
	if harnessErr.TestCaseID != testCaseID {
		t.Fatalf("test case: got %q want %q (%v)", harnessErr.TestCaseID, testCaseID, err)
	}
	return harnessErr
}

type ctxKey struct{}

func TestRunnerCallsPipelineOncePerCaseInManifestOrder(t *testing.T) {
	var calls []string
	ctx := context.WithValue(context.Background(), ctxKey{}, "propagated")
	pipeline := PipelineFunc(func(ctx context.Context, in PipelineInput) (PipelineOutput, error) {
		if ctx.Value(ctxKey{}) != "propagated" {
			t.Errorf("context not propagated for %s", in.TestCase.TestCase.TestCaseID)
		}
		if in.Config.ExperimentID != "E04" {
			t.Errorf("config not passed: %q", in.Config.ExperimentID)
		}
		calls = append(calls, in.TestCase.TestCase.TestCaseID)
		return PipelineOutput{}, nil
	})
	outcome, err := newRunner(t, pipeline).Run(ctx, runInput(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"football-live", "visual-vod"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls: %v want %v", calls, want)
	}
	var got []string
	for _, c := range outcome.Cases {
		got = append(got, c.TestCaseID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("outcome cases: %v", got)
	}
	meta := outcome.Metadata
	if meta.ProcessedTestCases != 2 || !meta.StartedAt.Equal(fixedStart) ||
		!meta.FinishedAt.Equal(fixedStart.Add(1500*time.Millisecond)) || meta.Elapsed != 1500*time.Millisecond {
		t.Fatalf("metadata: %#v", meta)
	}
	if outcome.Dataset != (config.DatasetIdentity{DatasetID: "poc-golden", DatasetVersion: "1.0", ManifestPath: fixtureManifest}) {
		t.Fatalf("dataset identity: %#v", outcome.Dataset)
	}
}

func TestPipelineFailureStopsAtFirstFailingCase(t *testing.T) {
	cause := errors.New("provider unavailable")
	var calls int
	pipeline := PipelineFunc(func(context.Context, PipelineInput) (PipelineOutput, error) {
		calls++
		return PipelineOutput{}, cause
	})
	_, err := newRunner(t, pipeline).Run(context.Background(), runInput(t))
	assertHarnessError(t, err, StagePipeline, "football-live")
	if !errors.Is(err, cause) {
		t.Fatalf("cause not wrapped: %v", err)
	}
	if calls != 1 {
		t.Fatalf("pipeline calls after failure: %d", calls)
	}
}

func TestCancellationPropagates(t *testing.T) {
	t.Run("before run", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		pipeline := PipelineFunc(func(context.Context, PipelineInput) (PipelineOutput, error) {
			t.Fatal("pipeline must not be called")
			return PipelineOutput{}, nil
		})
		_, err := newRunner(t, pipeline).Run(ctx, runInput(t))
		assertHarnessError(t, err, StageCancelled, "")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cause: %v", err)
		}
	})
	t.Run("inside pipeline", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		pipeline := PipelineFunc(func(ctx context.Context, _ PipelineInput) (PipelineOutput, error) {
			cancel()
			<-ctx.Done()
			return PipelineOutput{}, ctx.Err()
		})
		_, err := newRunner(t, pipeline).Run(ctx, runInput(t))
		assertHarnessError(t, err, StageCancelled, "football-live")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cause: %v", err)
		}
	})
	t.Run("between cases", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
		defer cancel()
		var calls int
		pipeline := PipelineFunc(func(context.Context, PipelineInput) (PipelineOutput, error) {
			calls++
			cancel()
			return PipelineOutput{}, nil
		})
		_, err := newRunner(t, pipeline).Run(ctx, runInput(t))
		assertHarnessError(t, err, StageCancelled, "visual-vod")
		if calls != 1 {
			t.Fatalf("calls: %d", calls)
		}
	})
	t.Run("validation-only pipeline observes deadline", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
		defer cancel()
		_, err := ValidationOnlyPipeline{}.Process(ctx, PipelineInput{})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error: %v", err)
		}
	})
}

func TestValidEvidenceBackedOutputIsAccepted(t *testing.T) {
	pipeline := PipelineFunc(func(_ context.Context, in PipelineInput) (PipelineOutput, error) {
		return evidenceBacked(in.TestCase.TestCase.Content), nil
	})
	outcome, err := newRunner(t, pipeline).Run(context.Background(), runInput(t))
	if err != nil {
		t.Fatal(err)
	}
	meta := outcome.Metadata
	if meta.AudioObservations != 2 || meta.VisualObservations != 2 || meta.ContextEvents != 2 {
		t.Fatalf("counts: %#v", meta)
	}
	if got := outcome.Cases[1].Output.ContextEvents[0].Content.ContentType; got != contracts.ContentTypeVOD {
		t.Fatalf("second case output content: %q", got)
	}
	if outcome.Result.Metrics != (contracts.ExperimentMetrics{}) {
		t.Fatalf("metrics must stay absent: %#v", outcome.Result.Metrics)
	}
}

func TestInvalidPipelineOutputFails(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*PipelineOutput)
		stage  Stage
		want   string
	}{
		{
			name:   "observation schema",
			mutate: func(o *PipelineOutput) { o.AudioObservations[0].ObservationID = "" },
			stage:  StageOutputSchema, want: "audioObservations[0]",
		},
		{
			name:   "visual detection schema",
			mutate: func(o *PipelineOutput) { o.VisualObservations[0].Frames[0].Observations[0].Type = "PRODUCT" },
			stage:  StageOutputSchema, want: "visualObservations[0]",
		},
		{
			name:   "event schema",
			mutate: func(o *PipelineOutput) { o.ContextEvents[0].Confidence = 1.5 },
			stage:  StageOutputSchema, want: "contextEvents[0]",
		},
		{
			name: "observation domain window",
			mutate: func(o *PipelineOutput) {
				o.AudioObservations[0].Window = contracts.TimeWindow{StartMs: 5000, EndMs: 1000}
			},
			stage: StageOutputDomain, want: "audioObservations[0]",
		},
		{
			name:   "event domain window",
			mutate: func(o *PipelineOutput) { o.ContextEvents[0].Window = contracts.TimeWindow{StartMs: 6000, EndMs: 5000} },
			stage:  StageOutputDomain, want: "contextEvents[0]",
		},
		{
			name: "event without evidence",
			mutate: func(o *PipelineOutput) {
				o.ContextEvents[0].Evidence = contracts.Evidence{Audio: []contracts.AudioEvidence{}, Visual: []contracts.VisualEvidence{}}
			},
			stage: StageOutputDomain, want: "evidence",
		},
		{
			name: "duplicate observation id",
			mutate: func(o *PipelineOutput) {
				o.VisualObservations[0].ObservationID = "aud-01"
				o.ContextEvents[0].Evidence.Visual[0].ObservationID = "aud-01"
			},
			stage: StageOutputDomain, want: "duplicate observationId",
		},
		{
			name:   "missing evidence reference",
			mutate: func(o *PipelineOutput) { o.ContextEvents[0].Evidence.Audio[0].ObservationID = "aud-missing" },
			stage:  StageOutputEvidence, want: "observationId not found",
		},
		{
			name:   "evidence kind mismatch",
			mutate: func(o *PipelineOutput) { o.ContextEvents[0].Evidence.Visual[0].ObservationID = "aud-01" },
			stage:  StageOutputEvidence, want: "refers to an audio observation",
		},
		{
			name: "observation content mismatch",
			mutate: func(o *PipelineOutput) {
				o.AudioObservations[0].Content = contracts.ContentRef{ContentID: "other", ContentType: contracts.ContentTypeLive}
			},
			stage: StageOutputContent, want: "does not match test case content",
		},
		{
			name: "event content type mismatch",
			mutate: func(o *PipelineOutput) {
				o.ContextEvents[0].Content.ContentType = contracts.ContentTypeVOD
			},
			stage: StageOutputContent, want: "contextEvents[0]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			pipeline := PipelineFunc(func(_ context.Context, in PipelineInput) (PipelineOutput, error) {
				calls++
				out := evidenceBacked(in.TestCase.TestCase.Content)
				tc.mutate(&out)
				return out, nil
			})
			_, err := newRunner(t, pipeline).Run(context.Background(), runInput(t))
			assertHarnessError(t, err, tc.stage, "football-live")
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err, tc.want)
			}
			if calls != 1 {
				t.Fatalf("calls after invalid output: %d", calls)
			}
		})
	}
}

func TestValidationOnlyRunProducesResultWithoutMetrics(t *testing.T) {
	in := runInput(t)
	outcome, err := newRunner(t, ValidationOnlyPipeline{}).Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	result := outcome.Result
	if result.ExperimentID != "E04" || !result.StartedAt.Equal(fixedStart) || result.StartedAt.Location() != time.UTC {
		t.Fatalf("result identity: %#v", result)
	}
	if result.Metrics != (contracts.ExperimentMetrics{}) {
		t.Fatalf("invented metrics: %#v", result.Metrics)
	}
	wantConfig := in.Config.Snapshot(config.DatasetIdentity{DatasetID: "poc-golden", DatasetVersion: "1.0", ManifestPath: fixtureManifest})
	if !reflect.DeepEqual(result.Configuration, wantConfig) {
		t.Fatalf("configuration: %#v", result.Configuration)
	}
	meta := outcome.Metadata
	if meta.ProcessedTestCases != 2 || meta.AudioObservations+meta.VisualObservations+meta.ContextEvents != 0 {
		t.Fatalf("metadata: %#v", meta)
	}
}

func TestRunIsDeterministic(t *testing.T) {
	pipeline := PipelineFunc(func(_ context.Context, in PipelineInput) (PipelineOutput, error) {
		return evidenceBacked(in.TestCase.TestCase.Content), nil
	})
	validator := newValidator(t)
	run := func() (RunOutcome, []byte) {
		r, err := NewRunner(validator, pipeline, WithClock(steppingClock(fixedStart, time.Second)))
		if err != nil {
			t.Fatal(err)
		}
		outcome, err := r.Run(context.Background(), runInput(t))
		if err != nil {
			t.Fatal(err)
		}
		data, err := EncodeResult(validator, outcome.Result)
		if err != nil {
			t.Fatal(err)
		}
		return outcome, data
	}
	first, firstJSON := run()
	second, secondJSON := run()
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("outcomes differ:\n%#v\n%#v", first, second)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("encoded results differ:\n%s\n%s", firstJSON, secondJSON)
	}
}

func TestRunnerRequiresDependencies(t *testing.T) {
	if _, err := NewRunner(nil, ValidationOnlyPipeline{}); err == nil {
		t.Fatal("nil validator accepted")
	}
	if _, err := NewRunner(newValidator(t), nil); err == nil {
		t.Fatal("nil pipeline accepted")
	}
}

func TestLoadDatasetUsesM02LoaderAndKeepsStageContext(t *testing.T) {
	specs := os.DirFS(specsRoot)
	t.Run("valid fixture keeps order", func(t *testing.T) {
		ds := loadFixture(t)
		if len(ds.TestCases) != 2 || ds.TestCases[0].TestCase.TestCaseID != "football-live" || ds.TestCases[1].TestCase.TestCaseID != "visual-vod" {
			t.Fatalf("dataset: %#v", ds.TestCases)
		}
	})
	cases := []struct {
		name     string
		dirFS    string
		manifest string
		layer    string
		want     string
	}{
		{name: "missing manifest", dirFS: datasetRoot, manifest: "manifests/missing.json", layer: dataset.LayerReference, want: "read manifest"},
		{name: "malformed manifest", dirFS: invalidDataRoot, manifest: "malformed-manifest.json", layer: dataset.LayerSchema},
		{name: "escaping manifest path", dirFS: datasetRoot, manifest: "../valid/manifests/poc-golden-v1.0.json", layer: dataset.LayerReference},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadDataset(os.DirFS(tc.dirFS), specs, tc.manifest)
			assertDatasetError(t, err, tc.layer, tc.want)
		})
	}
	t.Run("missing ground truth and media", func(t *testing.T) {
		manifest, err := os.ReadFile(filepath.Join(datasetRoot, fixtureManifest))
		if err != nil {
			t.Fatal(err)
		}
		groundTruth, err := os.ReadFile(filepath.Join(datasetRoot, "ground-truth", "football-live.json"))
		if err != nil {
			t.Fatal(err)
		}
		noGroundTruth := fstest.MapFS{fixtureManifest: {Data: manifest}}
		_, err = LoadDataset(noGroundTruth, specs, fixtureManifest)
		assertDatasetError(t, err, dataset.LayerReference, "read ground truth")

		noMedia := fstest.MapFS{
			fixtureManifest:                   {Data: manifest},
			"ground-truth/football-live.json": {Data: groundTruth},
		}
		_, err = LoadDataset(noMedia, specs, fixtureManifest)
		assertDatasetError(t, err, dataset.LayerReference, "read media")
	})
	t.Run("missing specs", func(t *testing.T) {
		_, err := LoadDataset(os.DirFS(datasetRoot), os.DirFS(t.TempDir()), fixtureManifest)
		assertHarnessError(t, err, StageSpecs, "")
	})
}

func assertDatasetError(t *testing.T, err error, layer, contains string) {
	t.Helper()
	assertHarnessError(t, err, StageDataset, "")
	var dsErr *dataset.Error
	var dsList dataset.ErrorList
	switch {
	case errors.As(err, &dsErr):
		if dsErr.Layer != layer {
			t.Fatalf("dataset layer: got %q want %q (%v)", dsErr.Layer, layer, err)
		}
	case errors.As(err, &dsList):
		if len(dsList) == 0 || dsList[0].Layer != layer {
			t.Fatalf("dataset layer: want %q (%v)", layer, err)
		}
	default:
		t.Fatalf("M02 error not wrapped: %T %v", err, err)
	}
	if contains != "" && !strings.Contains(err.Error(), contains) {
		t.Fatalf("error %q does not contain %q", err, contains)
	}
}
