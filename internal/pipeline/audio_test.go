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

	"github.com/ISR-Consulting/context-video/internal/audio"
	"github.com/ISR-Consulting/context-video/internal/evaluation/config"
	"github.com/ISR-Consulting/context-video/internal/evaluation/dataset"
	"github.com/ISR-Consulting/context-video/internal/evaluation/harness"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

var (
	specsRoot   = filepath.Join("..", "..", "specs")
	datasetRoot = filepath.Join("..", "evaluation", "dataset", "testdata", "valid")
)

const fixtureManifest = "manifests/poc-golden-v1.0.json"

// fakeTranscriber records requests and returns a deterministic transcription
// derived from the window, or the error chosen by fail.
type fakeTranscriber struct {
	mu       sync.Mutex
	requests []audio.Request
	fail     func(audio.Request) error
	mutate   func(*audio.Transcription)
}

func (f *fakeTranscriber) Transcribe(ctx context.Context, req audio.Request) (audio.Transcription, error) {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return audio.Transcription{}, err
	}
	if f.fail != nil {
		if err := f.fail(req); err != nil {
			return audio.Transcription{}, err
		}
	}
	t := audio.Transcription{
		Text:     fmt.Sprintf("speech %d-%d", req.Segment.Window.StartMs, req.Segment.Window.EndMs),
		Language: "pt",
		Provider: "fake",
		Model:    "fake-model",
	}
	if f.mutate != nil {
		f.mutate(&t)
	}
	return t, nil
}

func audioConfig(t *testing.T, window, provider string, vision bool) config.Config {
	t.Helper()
	visionYAML := "vision:\n  enabled: false\n"
	if vision {
		visionYAML = "vision:\n  enabled: true\n  provider: TBD\n  sampling: TBD\n"
	}
	yaml := fmt.Sprintf("experiment:\n  id: E01\n  name: audio-only\ningestion:\n  mode: LIVE_SIMULATION\n"+
		"window:\n  size: %s\naudio:\n  enabled: true\n  provider: %s\n%sfusion:\n  provider: TBD\nschema:\n  context_event: v1\n",
		window, provider, visionYAML)
	cfg, err := config.Parse("test.yaml", []byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func loadFixture(t *testing.T) dataset.Dataset {
	t.Helper()
	ds, err := harness.LoadDataset(os.DirFS(datasetRoot), os.DirFS(specsRoot), fixtureManifest)
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

func newAudio(t *testing.T, fake *fakeTranscriber) *Audio {
	t.Helper()
	p, err := NewAudio("fake", fake, DirResolver(datasetRoot))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func windows(obs []contracts.AudioObservation) []string {
	out := make([]string, len(obs))
	for i, o := range obs {
		out[i] = fmt.Sprintf("%d-%d", o.Window.StartMs, o.Window.EndMs)
	}
	return out
}

func TestAudioProcessEmitsOneObservationPerSegment(t *testing.T) {
	ds := loadFixture(t)
	cfg := audioConfig(t, "5s", "fake", false)
	fake := &fakeTranscriber{}
	p := newAudio(t, fake)

	football := ds.TestCases[0]
	out, err := p.Process(context.Background(), harness.PipelineInput{Config: cfg, TestCase: football})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.VisualObservations) != 0 || len(out.ContextEvents) != 0 {
		t.Fatalf("audio pipeline emitted non-audio output: %+v", out)
	}
	content := football.TestCase.Content
	want := []contracts.AudioObservation{
		{
			ObservationID: "aud:live-football-001:0-5000",
			Content:       content,
			Window:        contracts.TimeWindow{StartMs: 0, EndMs: 5000},
			Transcript:    contracts.Transcript{Text: "speech 0-5000", Language: ptr("pt")},
			Provenance:    contracts.ObservationProvenance{Provider: "fake", Model: ptr("fake-model"), PipelineVersion: DefaultPipelineVersion},
		},
		{
			ObservationID: "aud:live-football-001:5000-10000",
			Content:       content,
			Window:        contracts.TimeWindow{StartMs: 5000, EndMs: 10000},
			Transcript:    contracts.Transcript{Text: "speech 5000-10000", Language: ptr("pt")},
			Provenance:    contracts.ObservationProvenance{Provider: "fake", Model: ptr("fake-model"), PipelineVersion: DefaultPipelineVersion},
		},
	}
	if !reflect.DeepEqual(out.AudioObservations, want) {
		t.Fatalf("observations:\n%+v\nwant:\n%+v", out.AudioObservations, want)
	}

	wantSource := audio.Source{
		Kind: audio.SourceFixture,
		URI:  "media/football-live.fixture",
		Path: filepath.Join(datasetRoot, "media", "football-live.fixture"),
	}
	for i, req := range fake.requests {
		if req.Source != wantSource {
			t.Fatalf("request %d source: %+v", i, req.Source)
		}
		if req.Segment.SegmentID != "live-football-001:"+windows(want)[i] || *req.Segment.SourceURI != wantSource.URI {
			t.Fatalf("request %d segment: %+v", i, req.Segment)
		}
	}

	again, err := newAudio(t, &fakeTranscriber{}).Process(context.Background(), harness.PipelineInput{Config: cfg, TestCase: football})
	if err != nil || !reflect.DeepEqual(again, out) {
		t.Fatalf("not deterministic: %v", err)
	}
}

func TestAudioProcessWindowSizes(t *testing.T) {
	ds := loadFixture(t)
	cases := []struct {
		window string
		want   map[string][]string
	}{
		{"2s", map[string][]string{
			"football-live": {"0-2000", "2000-4000", "4000-6000", "6000-8000", "8000-10000"},
			"visual-vod":    {"0-2000", "2000-4000", "4000-6000", "6000-8000"},
		}},
		{"5s", map[string][]string{
			"football-live": {"0-5000", "5000-10000"},
			"visual-vod":    {"0-5000", "5000-8000"},
		}},
		{"10s", map[string][]string{
			"football-live": {"0-10000"},
			"visual-vod":    {"0-8000"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.window, func(t *testing.T) {
			cfg := audioConfig(t, tc.window, "fake", false)
			for _, testCase := range ds.TestCases {
				out, err := newAudio(t, &fakeTranscriber{}).Process(context.Background(), harness.PipelineInput{Config: cfg, TestCase: testCase})
				if err != nil {
					t.Fatal(err)
				}
				if got := windows(out.AudioObservations); !reflect.DeepEqual(got, tc.want[testCase.TestCase.TestCaseID]) {
					t.Fatalf("%s windows: %v", testCase.TestCase.TestCaseID, got)
				}
			}
		})
	}
}

func TestAudioProcessControlledSourceHasNoPath(t *testing.T) {
	ds := loadFixture(t)
	fake := &fakeTranscriber{}
	if _, err := newAudio(t, fake).Process(context.Background(), harness.PipelineInput{
		Config: audioConfig(t, "10s", "fake", false), TestCase: ds.TestCases[1],
	}); err != nil {
		t.Fatal(err)
	}
	want := audio.Source{Kind: audio.SourceControlledSource, URI: "controlled://approved/visual-vod-001"}
	if len(fake.requests) != 1 || fake.requests[0].Source != want {
		t.Fatalf("requests: %+v", fake.requests)
	}
}

func TestAudioProcessRejectsMismatchedConfig(t *testing.T) {
	ds := loadFixture(t)
	disabled := audioConfig(t, "5s", "fake", false)
	disabled.Audio = config.AudioConfig{}
	disabled.Vision = config.VisionConfig{Enabled: true, Provider: "TBD", Sampling: "TBD"}
	cases := map[string]struct {
		cfg  config.Config
		want string
	}{
		"audio disabled":    {disabled, "requires audio.enabled"},
		"vision enabled":    {audioConfig(t, "5s", "fake", true), "does not run vision"},
		"provider mismatch": {audioConfig(t, "5s", "whisper-cpp", false), `selects audio.provider "whisper-cpp" but the pipeline was built for "fake"`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fake := &fakeTranscriber{}
			_, err := newAudio(t, fake).Process(context.Background(), harness.PipelineInput{Config: tc.cfg, TestCase: ds.TestCases[0]})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v does not contain %q", err, tc.want)
			}
			if len(fake.requests) != 0 {
				t.Fatal("transcriber called")
			}
		})
	}
}

func TestAudioProcessErrors(t *testing.T) {
	ds := loadFixture(t)
	cfg := audioConfig(t, "5s", "fake", false)
	in := harness.PipelineInput{Config: cfg, TestCase: ds.TestCases[1]}

	t.Run("unsupported source propagates", func(t *testing.T) {
		fake := &fakeTranscriber{fail: func(req audio.Request) error {
			if req.Source.Kind == audio.SourceControlledSource {
				return fmt.Errorf("%w: controlled", audio.ErrUnsupportedSource)
			}
			return nil
		}}
		_, err := newAudio(t, fake).Process(context.Background(), in)
		if !errors.Is(err, audio.ErrUnsupportedSource) || !strings.Contains(err.Error(), "segment vod-visual-001:0-5000") {
			t.Fatalf("err: %v", err)
		}
		if len(fake.requests) != 1 {
			t.Fatalf("continued after failure: %d requests", len(fake.requests))
		}
	})
	t.Run("invalid transcription", func(t *testing.T) {
		fake := &fakeTranscriber{mutate: func(tr *audio.Transcription) { c := 1.5; tr.Confidence = &c }}
		if _, err := newAudio(t, fake).Process(context.Background(), in); err == nil || !strings.Contains(err.Error(), "confidence") {
			t.Fatalf("err: %v", err)
		}
	})
	t.Run("resolver failure", func(t *testing.T) {
		p, err := NewAudio("fake", &fakeTranscriber{}, func(dataset.MediaReference) (audio.Source, error) {
			return audio.Source{}, errors.New("nope")
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
		fake := &fakeTranscriber{fail: func(audio.Request) error { cancel(); return nil }}
		_, err := newAudio(t, fake).Process(ctx, in)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err: %v", err)
		}
		if len(fake.requests) != 1 {
			t.Fatalf("requests after cancel: %d", len(fake.requests))
		}
	})
}

func TestNewAudioValidatesArguments(t *testing.T) {
	resolve := DirResolver(datasetRoot)
	fake := &fakeTranscriber{}
	for name, build := range map[string]func() (*Audio, error){
		"blank provider":  func() (*Audio, error) { return NewAudio(" ", fake, resolve) },
		"nil transcriber": func() (*Audio, error) { return NewAudio("fake", nil, resolve) },
		"nil resolver":    func() (*Audio, error) { return NewAudio("fake", fake, nil) },
		"blank version":   func() (*Audio, error) { return NewAudio("fake", fake, resolve, WithPipelineVersion("")) },
	} {
		if _, err := build(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	p, err := NewAudio("fake", fake, resolve, WithPipelineVersion("poc-v1-test"))
	if err != nil || p.pipelineVersion != "poc-v1-test" {
		t.Fatalf("version option: %v", err)
	}
}

func TestDirResolver(t *testing.T) {
	resolve := DirResolver("/data")
	src, err := resolve(dataset.MediaReference{Kind: dataset.MediaKindLocal, URI: "media/a/b.mp4"})
	if err != nil || src != (audio.Source{Kind: audio.SourceLocal, URI: "media/a/b.mp4", Path: filepath.Join("/data", "media", "a", "b.mp4")}) {
		t.Fatalf("local: %+v %v", src, err)
	}
	if _, err := resolve(dataset.MediaReference{Kind: "S3", URI: "x"}); err == nil {
		t.Fatal("unknown kind accepted")
	}
}

func TestAudioPipelineThroughHarnessRunner(t *testing.T) {
	validator, err := contracts.NewValidator(os.DirFS(specsRoot))
	if err != nil {
		t.Fatal(err)
	}
	runner, err := harness.NewRunner(validator, newAudio(t, &fakeTranscriber{}))
	if err != nil {
		t.Fatal(err)
	}
	cfg := audioConfig(t, "5s", "fake", false)
	outcome, err := runner.Run(context.Background(), harness.RunInput{Config: cfg, Dataset: loadFixture(t), ManifestPath: fixtureManifest})
	if err != nil {
		t.Fatal(err)
	}
	meta := outcome.Metadata
	if meta.ProcessedTestCases != 2 || meta.AudioObservations != 4 || meta.VisualObservations != 0 || meta.ContextEvents != 0 {
		t.Fatalf("metadata: %+v", meta)
	}
	if outcome.Result.Metrics != (contracts.ExperimentMetrics{}) {
		t.Fatalf("metrics fabricated: %+v", outcome.Result.Metrics)
	}
	if got := outcome.Result.Configuration["audio"]; !reflect.DeepEqual(got, map[string]any{"enabled": true, "provider": "fake"}) {
		t.Fatalf("configuration audio: %v", got)
	}

	failing, err := harness.NewRunner(validator, newAudio(t, &fakeTranscriber{fail: func(req audio.Request) error {
		if req.Source.Kind == audio.SourceControlledSource {
			return audio.ErrUnsupportedSource
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

func ptr[T any](v T) *T { return &v }
