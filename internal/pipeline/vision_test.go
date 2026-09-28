package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/ISR-Consulting/context-video/internal/evaluation/config"
	"github.com/ISR-Consulting/context-video/internal/evaluation/dataset"
	"github.com/ISR-Consulting/context-video/internal/evaluation/harness"
	"github.com/ISR-Consulting/context-video/internal/vision"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// fakeAnalyzer records requests and returns one deterministic detection per
// requested frame, or the error chosen by fail.
type fakeAnalyzer struct {
	mu       sync.Mutex
	requests []vision.Request
	fail     func(vision.Request) error
	mutate   func(*vision.Analysis)
}

func (f *fakeAnalyzer) Analyze(ctx context.Context, req vision.Request) (vision.Analysis, error) {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return vision.Analysis{}, err
	}
	if f.fail != nil {
		if err := f.fail(req); err != nil {
			return vision.Analysis{}, err
		}
	}
	a := vision.Analysis{Provider: "fake", Model: "fake-vlm"}
	for _, ts := range req.FrameTimesMs {
		confidence := 0.8
		a.Frames = append(a.Frames, vision.Frame{
			TimestampMs: ts,
			Description: fmt.Sprintf("frame %d", ts),
			Detections:  []vision.Detection{{Type: contracts.VisualDetectionObject, Value: "football", Confidence: &confidence}},
		})
	}
	if f.mutate != nil {
		f.mutate(&a)
	}
	return a, nil
}

func visionConfig(t *testing.T, window, provider, sampling string, audio bool) config.Config {
	t.Helper()
	audioYAML := "audio:\n  enabled: false\n"
	if audio {
		audioYAML = "audio:\n  enabled: true\n  provider: TBD\n"
	}
	yaml := fmt.Sprintf("experiment:\n  id: E02\n  name: vision-only\ningestion:\n  mode: LIVE_SIMULATION\n"+
		"window:\n  size: %s\n%svision:\n  enabled: true\n  provider: %s\n  sampling: %s\nfusion:\n  provider: TBD\nschema:\n  context_event: v1\n",
		window, audioYAML, provider, sampling)
	cfg, err := config.Parse("test.yaml", []byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func newVision(t *testing.T, fake *fakeAnalyzer) *Vision {
	t.Helper()
	p, err := NewVision("fake", fake, VisionDirResolver(datasetRoot))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func visualWindows(obs []contracts.VisualObservation) []string {
	out := make([]string, len(obs))
	for i, o := range obs {
		out[i] = fmt.Sprintf("%d-%d", o.Window.StartMs, o.Window.EndMs)
	}
	return out
}

func frameTimes(obs contracts.VisualObservation) []int64 {
	out := make([]int64, len(obs.Frames))
	for i, f := range obs.Frames {
		out[i] = f.TimestampMs
	}
	return out
}

func TestVisionProcessEmitsOneObservationPerSegment(t *testing.T) {
	ds := loadFixture(t)
	cfg := visionConfig(t, "5s", "fake", "uniform:2", false)
	fake := &fakeAnalyzer{}
	football := ds.TestCases[0]
	out, err := newVision(t, fake).Process(context.Background(), harness.PipelineInput{Config: cfg, TestCase: football})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.AudioObservations) != 0 || len(out.ContextEvents) != 0 {
		t.Fatalf("vision pipeline emitted non-visual output: %+v", out)
	}
	frame := func(ts int64) contracts.VisualFrame {
		return contracts.VisualFrame{
			TimestampMs:  ts,
			Description:  ptr(fmt.Sprintf("frame %d", ts)),
			Observations: []contracts.VisualDetection{{Type: contracts.VisualDetectionObject, Value: "football", Confidence: 0.8}},
		}
	}
	provenance := contracts.ObservationProvenance{Provider: "fake", Model: ptr("fake-vlm"), PipelineVersion: DefaultPipelineVersion}
	content := football.TestCase.Content
	want := []contracts.VisualObservation{
		{
			ObservationID: "vis:live-football-001:0-5000",
			Content:       content,
			Window:        contracts.TimeWindow{StartMs: 0, EndMs: 5000},
			Frames:        []contracts.VisualFrame{frame(1250), frame(3750)},
			Provenance:    provenance,
		},
		{
			ObservationID: "vis:live-football-001:5000-10000",
			Content:       content,
			Window:        contracts.TimeWindow{StartMs: 5000, EndMs: 10000},
			Frames:        []contracts.VisualFrame{frame(6250), frame(8750)},
			Provenance:    provenance,
		},
	}
	if !reflect.DeepEqual(out.VisualObservations, want) {
		t.Fatalf("observations:\n%+v\nwant:\n%+v", out.VisualObservations, want)
	}

	wantSource := vision.Source{
		Kind: vision.SourceFixture,
		URI:  "media/football-live.fixture",
		Path: filepath.Join(datasetRoot, "media", "football-live.fixture"),
	}
	for i, req := range fake.requests {
		if req.Source != wantSource {
			t.Fatalf("request %d source: %+v", i, req.Source)
		}
		if req.Segment.SegmentID != "live-football-001:"+visualWindows(want)[i] || *req.Segment.SourceURI != wantSource.URI {
			t.Fatalf("request %d segment: %+v", i, req.Segment)
		}
		if !reflect.DeepEqual(req.FrameTimesMs, frameTimes(want[i])) {
			t.Fatalf("request %d frames: %v", i, req.FrameTimesMs)
		}
	}

	again, err := newVision(t, &fakeAnalyzer{}).Process(context.Background(), harness.PipelineInput{Config: cfg, TestCase: football})
	if err != nil || !reflect.DeepEqual(again, out) {
		t.Fatalf("not deterministic: %v", err)
	}
}

func TestVisionProcessWindowSizesAndSampling(t *testing.T) {
	ds := loadFixture(t)
	cases := []struct {
		window, sampling string
		want             map[string][]string
		frames           map[string][][]int64
	}{
		{"2s", "uniform:1", map[string][]string{
			"football-live": {"0-2000", "2000-4000", "4000-6000", "6000-8000", "8000-10000"},
			"visual-vod":    {"0-2000", "2000-4000", "4000-6000", "6000-8000"},
		}, map[string][][]int64{
			"football-live": {{1000}, {3000}, {5000}, {7000}, {9000}},
			"visual-vod":    {{1000}, {3000}, {5000}, {7000}},
		}},
		{"5s", "uniform:2", map[string][]string{
			"football-live": {"0-5000", "5000-10000"},
			"visual-vod":    {"0-5000", "5000-8000"},
		}, map[string][][]int64{
			"football-live": {{1250, 3750}, {6250, 8750}},
			"visual-vod":    {{1250, 3750}, {5750, 7250}},
		}},
		{"10s", "uniform:4", map[string][]string{
			"football-live": {"0-10000"},
			"visual-vod":    {"0-8000"},
		}, map[string][][]int64{
			"football-live": {{1250, 3750, 6250, 8750}},
			"visual-vod":    {{1000, 3000, 5000, 7000}},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.window, func(t *testing.T) {
			cfg := visionConfig(t, tc.window, "fake", tc.sampling, false)
			for _, testCase := range ds.TestCases {
				id := testCase.TestCase.TestCaseID
				out, err := newVision(t, &fakeAnalyzer{}).Process(context.Background(), harness.PipelineInput{Config: cfg, TestCase: testCase})
				if err != nil {
					t.Fatal(err)
				}
				if got := visualWindows(out.VisualObservations); !reflect.DeepEqual(got, tc.want[id]) {
					t.Fatalf("%s windows: %v", id, got)
				}
				for i, obs := range out.VisualObservations {
					if got := frameTimes(obs); !reflect.DeepEqual(got, tc.frames[id][i]) {
						t.Fatalf("%s window %d frames: %v", id, i, got)
					}
					if err := contracts.Validate(obs); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}

func TestVisionProcessControlledSourceHasNoPath(t *testing.T) {
	ds := loadFixture(t)
	fake := &fakeAnalyzer{}
	if _, err := newVision(t, fake).Process(context.Background(), harness.PipelineInput{
		Config: visionConfig(t, "10s", "fake", "uniform:1", false), TestCase: ds.TestCases[1],
	}); err != nil {
		t.Fatal(err)
	}
	want := vision.Source{Kind: vision.SourceControlledSource, URI: "controlled://approved/visual-vod-001"}
	if len(fake.requests) != 1 || fake.requests[0].Source != want {
		t.Fatalf("requests: %+v", fake.requests)
	}
}

func TestVisionProcessRejectsMismatchedConfig(t *testing.T) {
	ds := loadFixture(t)
	disabled := visionConfig(t, "5s", "fake", "uniform:1", false)
	disabled.Vision = config.VisionConfig{}
	disabled.Audio = config.AudioConfig{Enabled: true, Provider: "TBD"}
	cases := map[string]struct {
		cfg  config.Config
		want string
	}{
		"vision disabled":   {disabled, "requires vision.enabled"},
		"audio enabled":     {visionConfig(t, "5s", "fake", "uniform:1", true), "does not run audio"},
		"provider mismatch": {visionConfig(t, "5s", "llama-mtmd", "uniform:1", false), `selects vision.provider "llama-mtmd" but the pipeline was built for "fake"`},
		"sampling TBD":      {visionConfig(t, "5s", "fake", "TBD", false), `vision.sampling: unsupported vision sampling policy "TBD"`},
		"sampling scene":    {visionConfig(t, "5s", "fake", "scene:0.3", false), "supported: uniform:N"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fake := &fakeAnalyzer{}
			_, err := newVision(t, fake).Process(context.Background(), harness.PipelineInput{Config: tc.cfg, TestCase: ds.TestCases[0]})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v does not contain %q", err, tc.want)
			}
			if len(fake.requests) != 0 {
				t.Fatal("analyzer called")
			}
		})
	}
}

func TestVisionProcessErrors(t *testing.T) {
	ds := loadFixture(t)
	cfg := visionConfig(t, "5s", "fake", "uniform:2", false)
	in := harness.PipelineInput{Config: cfg, TestCase: ds.TestCases[1]}

	t.Run("unsupported source propagates", func(t *testing.T) {
		fake := &fakeAnalyzer{fail: func(req vision.Request) error {
			if req.Source.Kind == vision.SourceControlledSource {
				return fmt.Errorf("%w: controlled", vision.ErrUnsupportedSource)
			}
			return nil
		}}
		_, err := newVision(t, fake).Process(context.Background(), in)
		if !errors.Is(err, vision.ErrUnsupportedSource) || !strings.Contains(err.Error(), "segment vod-visual-001:0-5000") {
			t.Fatalf("err: %v", err)
		}
		if len(fake.requests) != 1 {
			t.Fatalf("continued after failure: %d requests", len(fake.requests))
		}
	})
	t.Run("missing confidence rejected", func(t *testing.T) {
		fake := &fakeAnalyzer{mutate: func(a *vision.Analysis) { a.Frames[0].Detections[0].Confidence = nil }}
		if _, err := newVision(t, fake).Process(context.Background(), in); err == nil || !strings.Contains(err.Error(), "confidence is required") {
			t.Fatalf("err: %v", err)
		}
	})
	t.Run("dropped frame rejected", func(t *testing.T) {
		fake := &fakeAnalyzer{mutate: func(a *vision.Analysis) { a.Frames = a.Frames[1:] }}
		if _, err := newVision(t, fake).Process(context.Background(), in); err == nil || !strings.Contains(err.Error(), "1 frames for 2") {
			t.Fatalf("err: %v", err)
		}
	})
	t.Run("resolver failure", func(t *testing.T) {
		p, err := NewVision("fake", &fakeAnalyzer{}, func(dataset.MediaReference) (vision.Source, error) {
			return vision.Source{}, errors.New("nope")
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Process(context.Background(), in); err == nil || !strings.Contains(err.Error(), "resolve media") {
			t.Fatalf("err: %v", err)
		}
	})
	t.Run("cancellation between segments", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		fake := &fakeAnalyzer{fail: func(vision.Request) error { cancel(); return nil }}
		_, err := newVision(t, fake).Process(ctx, in)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err: %v", err)
		}
		if len(fake.requests) != 1 {
			t.Fatalf("requests after cancel: %d", len(fake.requests))
		}
	})
}

func TestNewVisionValidatesArguments(t *testing.T) {
	resolve := VisionDirResolver(datasetRoot)
	fake := &fakeAnalyzer{}
	for name, build := range map[string]func() (*Vision, error){
		"blank provider": func() (*Vision, error) { return NewVision(" ", fake, resolve) },
		"nil analyzer":   func() (*Vision, error) { return NewVision("fake", nil, resolve) },
		"nil resolver":   func() (*Vision, error) { return NewVision("fake", fake, nil) },
		"blank version":  func() (*Vision, error) { return NewVision("fake", fake, resolve, WithVisionPipelineVersion("")) },
	} {
		if _, err := build(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	p, err := NewVision("fake", fake, resolve, WithVisionPipelineVersion("poc-v1-test"))
	if err != nil || p.pipelineVersion != "poc-v1-test" {
		t.Fatalf("version option: %v", err)
	}
}

func TestVisionDirResolver(t *testing.T) {
	resolve := VisionDirResolver("/data")
	src, err := resolve(dataset.MediaReference{Kind: dataset.MediaKindLocal, URI: "media/a/b.mp4"})
	if err != nil || src != (vision.Source{Kind: vision.SourceLocal, URI: "media/a/b.mp4", Path: filepath.Join("/data", "media", "a", "b.mp4")}) {
		t.Fatalf("local: %+v %v", src, err)
	}
	src, err = resolve(dataset.MediaReference{Kind: dataset.MediaKindControlledSource, URI: "controlled://x"})
	if err != nil || src.Path != "" {
		t.Fatalf("controlled: %+v %v", src, err)
	}
	if _, err := resolve(dataset.MediaReference{Kind: "S3", URI: "x"}); err == nil {
		t.Fatal("unknown kind accepted")
	}
}

func TestVisionPipelineThroughHarnessRunner(t *testing.T) {
	validator, err := contracts.NewValidator(os.DirFS(specsRoot))
	if err != nil {
		t.Fatal(err)
	}
	runner, err := harness.NewRunner(validator, newVision(t, &fakeAnalyzer{}))
	if err != nil {
		t.Fatal(err)
	}
	cfg := visionConfig(t, "5s", "fake", "uniform:2", false)
	outcome, err := runner.Run(context.Background(), harness.RunInput{Config: cfg, Dataset: loadFixture(t), ManifestPath: fixtureManifest})
	if err != nil {
		t.Fatal(err)
	}
	meta := outcome.Metadata
	if meta.ProcessedTestCases != 2 || meta.VisualObservations != 4 || meta.AudioObservations != 0 || meta.ContextEvents != 0 {
		t.Fatalf("metadata: %+v", meta)
	}
	if outcome.Result.Metrics != (contracts.ExperimentMetrics{}) {
		t.Fatalf("metrics fabricated: %+v", outcome.Result.Metrics)
	}
	if got := outcome.Result.Configuration["vision"]; !reflect.DeepEqual(got, map[string]any{"enabled": true, "provider": "fake", "sampling": "uniform:2"}) {
		t.Fatalf("configuration vision: %v", got)
	}

	failing, err := harness.NewRunner(validator, newVision(t, &fakeAnalyzer{fail: func(req vision.Request) error {
		if req.Source.Kind == vision.SourceControlledSource {
			return vision.ErrUnsupportedSource
		}
		return nil
	}}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = failing.Run(context.Background(), harness.RunInput{Config: cfg, Dataset: loadFixture(t), ManifestPath: fixtureManifest})
	var harnessErr *harness.Error
	if !errors.As(err, &harnessErr) || harnessErr.Stage != harness.StagePipeline || harnessErr.TestCaseID != "visual-vod" {
		t.Fatalf("err: %v", err)
	}
}
