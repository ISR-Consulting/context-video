package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/ISR-Consulting/context-video/internal/audio"
	contextcore "github.com/ISR-Consulting/context-video/internal/context"
	"github.com/ISR-Consulting/context-video/internal/evaluation/config"
	"github.com/ISR-Consulting/context-video/internal/evaluation/dataset"
	"github.com/ISR-Consulting/context-video/internal/evaluation/harness"
	"github.com/ISR-Consulting/context-video/internal/vision"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// callLog records the order in which perception and reasoning run.
type callLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *callLog) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, s)
}

// citingReasoner concludes one "football" topic per group and cites every
// piece of evidence in it, or fails as chosen.
type citingReasoner struct {
	log   *callLog
	calls int
	fail  error
}

func (r *citingReasoner) Reason(ctx context.Context, g contextcore.EvidenceGroup) ([]contextcore.Candidate, error) {
	r.calls++
	if r.log != nil {
		r.log.add("reason " + g.SegmentID)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.fail != nil {
		return nil, r.fail
	}
	var refs []contextcore.EvidenceRef
	seen := map[string]bool{}
	for _, e := range g.Evidence {
		ref := e.Ref()
		key := fmt.Sprintf("%s@%v", ref.ObservationID, e.TimestampMs != nil)
		if e.TimestampMs != nil {
			key = fmt.Sprintf("%s@%d", ref.ObservationID, *e.TimestampMs)
		}
		if !seen[key] {
			seen[key] = true
			refs = append(refs, ref)
		}
	}
	return []contextcore.Candidate{{
		Topics:     []contextcore.Value{{Value: "football", Confidence: ptr(0.9)}},
		Confidence: ptr(0.8),
		Evidence:   refs,
		Reasoning:  contextcore.ReasoningMetadata{Provider: "fake-reasoner", Model: "fake-llm", PromptVersion: "test-v1"},
	}}, nil
}

func multimodalConfig(t *testing.T, window string, withAudio, withVision bool, fusion string) config.Config {
	t.Helper()
	audioYAML := "audio:\n  enabled: false\n"
	if withAudio {
		audioYAML = "audio:\n  enabled: true\n  provider: fake\n"
	}
	visionYAML := "vision:\n  enabled: false\n"
	if withVision {
		visionYAML = "vision:\n  enabled: true\n  provider: fake\n  sampling: uniform:2\n"
	}
	yaml := fmt.Sprintf("experiment:\n  id: E04\n  name: multimodal\ningestion:\n  mode: LIVE_SIMULATION\n"+
		"window:\n  size: %s\n%s%sfusion:\n  provider: %s\nschema:\n  context_event: v1\n",
		window, audioYAML, visionYAML, fusion)
	cfg, err := config.Parse("test.yaml", []byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

type mmParts struct {
	transcriber *fakeTranscriber
	analyzer    *fakeAnalyzer
	reasoner    contextcore.Reasoner
	engineOpts  []contextcore.EngineOption
}

func newMultimodal(t *testing.T, p mmParts) *Multimodal {
	t.Helper()
	if p.reasoner == nil {
		p.reasoner = &citingReasoner{}
	}
	engine, err := contextcore.NewEngine("fake-reasoner", p.reasoner, p.engineOpts...)
	if err != nil {
		t.Fatal(err)
	}
	cfg := MultimodalConfig{Engine: engine}
	if p.transcriber != nil {
		cfg.AudioProvider, cfg.Transcriber, cfg.AudioResolve = "fake", p.transcriber, DirResolver(datasetRoot)
	}
	if p.analyzer != nil {
		cfg.VisionProvider, cfg.Analyzer, cfg.VisionResolve = "fake", p.analyzer, VisionDirResolver(datasetRoot)
	}
	m, err := NewMultimodal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func footballCase(t *testing.T) dataset.LoadedTestCase {
	t.Helper()
	return loadFixture(t).TestCases[0]
}

func TestMultimodalAudioAndVision(t *testing.T) {
	log := &callLog{}
	transcriber := &fakeTranscriber{mutate: func(tr *audio.Transcription) { log.add("audio " + tr.Text) }}
	analyzer := &fakeAnalyzer{mutate: func(a *vision.Analysis) { log.add(fmt.Sprintf("vision %d", a.Frames[0].TimestampMs)) }}
	m := newMultimodal(t, mmParts{transcriber: transcriber, analyzer: analyzer, reasoner: &citingReasoner{log: log}})
	out, err := m.Process(context.Background(), harness.PipelineInput{
		Config: multimodalConfig(t, "5s", true, true, "fake-reasoner"), TestCase: footballCase(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	wantLog := []string{
		"audio speech 0-5000", "vision 1250", "reason live-football-001:0-5000",
		"audio speech 5000-10000", "vision 6250", "reason live-football-001:5000-10000",
	}
	if fmt.Sprint(log.calls) != fmt.Sprint(wantLog) {
		t.Fatalf("order %v\nwant %v", log.calls, wantLog)
	}
	if len(out.AudioObservations) != 2 || len(out.VisualObservations) != 2 || len(out.ContextEvents) != 2 {
		t.Fatalf("output: %d audio %d visual %d events", len(out.AudioObservations), len(out.VisualObservations), len(out.ContextEvents))
	}
	event := out.ContextEvents[1]
	if event.EventID != "ctx:live-football-001:5000-10000:1" || event.Window != (contracts.TimeWindow{StartMs: 5000, EndMs: 10000}) ||
		event.Content != out.AudioObservations[1].Content {
		t.Fatalf("event identity: %+v", event)
	}
	if len(event.Evidence.Audio) != 1 || event.Evidence.Audio[0].ObservationID != out.AudioObservations[1].ObservationID ||
		event.Evidence.Audio[0].Text != "speech 5000-10000" {
		t.Fatalf("audio evidence: %+v", event.Evidence.Audio)
	}
	if len(event.Evidence.Visual) != 2 || event.Evidence.Visual[0].TimestampMs != 6250 || event.Evidence.Visual[1].TimestampMs != 8750 ||
		event.Evidence.Visual[0].Description != "frame 6250" {
		t.Fatalf("visual evidence: %+v", event.Evidence.Visual)
	}
	if *event.Provenance.FusionProvider != "fake-reasoner" || *event.Provenance.FusionModel != "fake-llm" || *event.Provenance.PromptVersion != "test-v1" {
		t.Fatalf("provenance: %+v", event.Provenance)
	}
	catalog, err := contracts.NewCatalog(out.AudioObservations, out.VisualObservations)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range out.ContextEvents {
		if err := e.ValidateEvidence(catalog); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMultimodalSingleModality(t *testing.T) {
	ctx := context.Background()
	audioOnly := newMultimodal(t, mmParts{transcriber: &fakeTranscriber{}})
	out, err := audioOnly.Process(ctx, harness.PipelineInput{Config: multimodalConfig(t, "5s", true, false, "fake-reasoner"), TestCase: footballCase(t)})
	if err != nil || len(out.AudioObservations) != 2 || out.VisualObservations != nil || len(out.ContextEvents) != 2 ||
		len(out.ContextEvents[0].Evidence.Visual) != 0 || len(out.ContextEvents[0].Evidence.Audio) != 1 {
		t.Fatalf("audio only: %+v %v", out, err)
	}
	visionOnly := newMultimodal(t, mmParts{analyzer: &fakeAnalyzer{}})
	out, err = visionOnly.Process(ctx, harness.PipelineInput{Config: multimodalConfig(t, "10s", false, true, "fake-reasoner"), TestCase: footballCase(t)})
	if err != nil || out.AudioObservations != nil || len(out.VisualObservations) != 1 || len(out.ContextEvents) != 1 ||
		len(out.ContextEvents[0].Evidence.Audio) != 0 || len(out.ContextEvents[0].Evidence.Visual) != 2 {
		t.Fatalf("vision only: %+v %v", out, err)
	}
}

func TestMultimodalNoUsefulEvidence(t *testing.T) {
	reasoner := &citingReasoner{}
	m := newMultimodal(t, mmParts{
		transcriber: &fakeTranscriber{mutate: func(tr *audio.Transcription) { tr.Text = "" }},
		analyzer: &fakeAnalyzer{mutate: func(a *vision.Analysis) {
			for i := range a.Frames {
				a.Frames[i].Description, a.Frames[i].Detections = "", nil
			}
		}},
		reasoner: reasoner,
	})
	out, err := m.Process(context.Background(), harness.PipelineInput{Config: multimodalConfig(t, "5s", true, true, "fake-reasoner"), TestCase: footballCase(t)})
	if err != nil || len(out.ContextEvents) != 0 || reasoner.calls != 0 || len(out.AudioObservations) != 2 || len(out.VisualObservations) != 2 {
		t.Fatalf("out %+v err %v reasoner calls %d", out, err, reasoner.calls)
	}
}

func TestMultimodalDeterministic(t *testing.T) {
	in := harness.PipelineInput{Config: multimodalConfig(t, "2s", true, true, "fake-reasoner"), TestCase: footballCase(t)}
	var first []byte
	for i := range 3 {
		m := newMultimodal(t, mmParts{transcriber: &fakeTranscriber{}, analyzer: &fakeAnalyzer{}})
		out, err := m.Process(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(out)
		if i == 0 {
			first = data
			continue
		}
		if string(data) != string(first) {
			t.Fatal("multimodal output differs between runs")
		}
	}
}

func TestMultimodalRejectsMismatchedConfig(t *testing.T) {
	both := newMultimodal(t, mmParts{transcriber: &fakeTranscriber{}, analyzer: &fakeAnalyzer{}})
	audioOnly := newMultimodal(t, mmParts{transcriber: &fakeTranscriber{}})
	tbdSampling := multimodalConfig(t, "5s", true, true, "fake-reasoner")
	tbdSampling.Vision.Sampling = "TBD"
	otherAudio := multimodalConfig(t, "5s", true, true, "fake-reasoner")
	otherAudio.Audio.Provider = "whisper-cpp"
	otherVision := multimodalConfig(t, "5s", true, true, "fake-reasoner")
	otherVision.Vision.Provider = "llama-mtmd"
	cases := []struct {
		name string
		m    *Multimodal
		cfg  config.Config
		want string
	}{
		{"fusion provider", both, multimodalConfig(t, "5s", true, true, "TBD"), `fusion.provider "TBD"`},
		{"vision disabled but wired", both, multimodalConfig(t, "5s", true, false, "fake-reasoner"), "vision.enabled: false"},
		{"vision enabled but not wired", audioOnly, multimodalConfig(t, "5s", true, true, "fake-reasoner"), "vision.enabled: true"},
		{"audio disabled but wired", both, multimodalConfig(t, "5s", false, true, "fake-reasoner"), "audio.enabled: false"},
		{"audio provider", both, otherAudio, `audio.provider "whisper-cpp"`},
		{"vision provider", both, otherVision, `vision.provider "llama-mtmd"`},
		{"sampling", both, tbdSampling, "vision.sampling"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.m.Process(context.Background(), harness.PipelineInput{Config: tc.cfg, TestCase: footballCase(t)})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v does not contain %q", err, tc.want)
			}
		})
	}
}

func TestNewMultimodalValidates(t *testing.T) {
	engine, err := contextcore.NewEngine("fake-reasoner", &citingReasoner{})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]MultimodalConfig{
		"no engine":          {AudioProvider: "fake", Transcriber: &fakeTranscriber{}, AudioResolve: DirResolver(datasetRoot)},
		"no modality":        {Engine: engine},
		"audio without port": {Engine: engine, AudioProvider: "fake", AudioResolve: DirResolver(datasetRoot)},
		"vision no resolver": {Engine: engine, VisionProvider: "fake", Analyzer: &fakeAnalyzer{}},
		"vision no provider": {Engine: engine, Analyzer: &fakeAnalyzer{}, VisionResolve: VisionDirResolver(datasetRoot)},
	}
	for name, cfg := range cases {
		if _, err := NewMultimodal(cfg); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	ok := MultimodalConfig{Engine: engine, AudioProvider: "fake", Transcriber: &fakeTranscriber{}, AudioResolve: DirResolver(datasetRoot)}
	if _, err := NewMultimodal(ok, WithMultimodalPipelineVersion(" ")); err == nil {
		t.Error("blank pipeline version accepted")
	}
	m, err := NewMultimodal(ok, WithMultimodalPipelineVersion("poc-v9"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := m.Process(context.Background(), harness.PipelineInput{Config: multimodalConfig(t, "5s", true, false, "fake-reasoner"), TestCase: footballCase(t)})
	if err != nil || out.AudioObservations[0].Provenance.PipelineVersion != "poc-v9" {
		t.Fatalf("%v %v", out, err)
	}
}

func TestMultimodalStageErrors(t *testing.T) {
	cause := errors.New("provider down")
	failAt := func(start int64) func(int64) error {
		return func(s int64) error {
			if s == start {
				return cause
			}
			return nil
		}
	}
	audioFail := failAt(5000)
	visionFail := failAt(5000)
	cases := []struct {
		name  string
		parts mmParts
		stage contextcore.Stage
	}{
		{"audio", mmParts{
			transcriber: &fakeTranscriber{fail: func(r audio.Request) error { return audioFail(r.Segment.Window.StartMs) }},
			analyzer:    &fakeAnalyzer{},
		}, contextcore.StageAudio},
		{"audio mapping", mmParts{
			transcriber: &fakeTranscriber{mutate: func(tr *audio.Transcription) { tr.Provider = "" }},
			analyzer:    &fakeAnalyzer{},
		}, contextcore.StageAudio},
		{"vision", mmParts{
			transcriber: &fakeTranscriber{},
			analyzer:    &fakeAnalyzer{fail: func(r vision.Request) error { return visionFail(r.Segment.Window.StartMs) }},
		}, contextcore.StageVision},
		{"vision mapping", mmParts{
			transcriber: &fakeTranscriber{},
			analyzer:    &fakeAnalyzer{mutate: func(a *vision.Analysis) { a.Frames = a.Frames[:1] }},
		}, contextcore.StageVision},
		{"reason", mmParts{
			transcriber: &fakeTranscriber{}, analyzer: &fakeAnalyzer{}, reasoner: &citingReasoner{fail: cause},
		}, contextcore.StageReason},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := newMultimodal(t, tc.parts).Process(context.Background(), harness.PipelineInput{
				Config: multimodalConfig(t, "5s", true, true, "fake-reasoner"), TestCase: footballCase(t),
			})
			var staged *contextcore.Error
			if !errors.As(err, &staged) || staged.Stage != tc.stage || staged.SegmentID == "" || staged.ContentID != "live-football-001" {
				t.Fatalf("err %v", err)
			}
			if out.ContextEvents != nil || out.AudioObservations != nil {
				t.Fatalf("partial output returned: %+v", out)
			}
			if strings.HasSuffix(tc.name, "mapping") {
				return
			}
			if !errors.Is(err, cause) || !strings.Contains(err.Error(), "provider fake") {
				t.Fatalf("cause lost: %v", err)
			}
		})
	}
}

func TestMultimodalCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	in := harness.PipelineInput{Config: multimodalConfig(t, "5s", true, true, "fake-reasoner"), TestCase: footballCase(t)}
	transcriber := &fakeTranscriber{}
	if _, err := newMultimodal(t, mmParts{transcriber: transcriber, analyzer: &fakeAnalyzer{}}).Process(ctx, in); !errors.Is(err, ctx.Err()) || len(transcriber.requests) != 0 {
		t.Fatalf("pre-cancelled: %v", err)
	}

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	cancelling := contextcore.ReasonerFunc(func(ctx context.Context, _ contextcore.EvidenceGroup) ([]contextcore.Candidate, error) {
		cancel()
		return nil, ctx.Err()
	})
	_, err := newMultimodal(t, mmParts{transcriber: &fakeTranscriber{}, analyzer: &fakeAnalyzer{}, reasoner: cancelling}).Process(ctx, in)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("in-flight: %v", err)
	}
}

// forgingCorrelator wraps SegmentCorrelator and renames the audio evidence to
// an observation the pipeline never emitted. The engine grounds against the
// forged group, so only the M03 runner's catalog check can catch it.
type forgingCorrelator struct{}

func (forgingCorrelator) Correlate(in contextcore.CorrelationInput) ([]contextcore.EvidenceGroup, error) {
	groups, err := contextcore.SegmentCorrelator{}.Correlate(in)
	for _, g := range groups {
		for i := range g.Evidence {
			if g.Evidence[i].Kind == contextcore.EvidenceAudio {
				g.Evidence[i].ObservationID = "aud:ghost"
			}
		}
	}
	return groups, err
}

func TestMultimodalThroughHarnessRunner(t *testing.T) {
	validator, err := contracts.NewValidator(os.DirFS(specsRoot))
	if err != nil {
		t.Fatal(err)
	}
	cfg := multimodalConfig(t, "5s", true, true, "fake-reasoner")
	runner, err := harness.NewRunner(validator, newMultimodal(t, mmParts{transcriber: &fakeTranscriber{}, analyzer: &fakeAnalyzer{}}))
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := runner.Run(context.Background(), harness.RunInput{Config: cfg, Dataset: loadFixture(t), ManifestPath: fixtureManifest})
	if err != nil {
		t.Fatal(err)
	}
	meta := outcome.Metadata
	if meta.ProcessedTestCases != 2 || meta.AudioObservations != 4 || meta.VisualObservations != 4 || meta.ContextEvents != 4 {
		t.Fatalf("metadata: %+v", meta)
	}
	if outcome.Result.Metrics != (contracts.ExperimentMetrics{}) {
		t.Fatalf("metrics fabricated: %+v", outcome.Result.Metrics)
	}
	if got := outcome.Result.Configuration["fusion"]; fmt.Sprint(got) != "map[provider:fake-reasoner]" {
		t.Fatalf("configuration fusion: %v", got)
	}

	forged := newMultimodal(t, mmParts{
		transcriber: &fakeTranscriber{}, analyzer: &fakeAnalyzer{},
		engineOpts: []contextcore.EngineOption{contextcore.WithCorrelator(forgingCorrelator{})},
	})
	runner, err = harness.NewRunner(validator, forged)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Run(context.Background(), harness.RunInput{Config: cfg, Dataset: loadFixture(t), ManifestPath: fixtureManifest})
	var harnessErr *harness.Error
	if !errors.As(err, &harnessErr) || harnessErr.Stage != harness.StageOutputEvidence || !strings.Contains(err.Error(), "observationId not found") {
		t.Fatalf("runner accepted an event citing an unknown observation: %v", err)
	}
}
