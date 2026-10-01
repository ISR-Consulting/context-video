package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ISR-Consulting/context-video/internal/audio/providers"
	contextcore "github.com/ISR-Consulting/context-video/internal/context"
	"github.com/ISR-Consulting/context-video/internal/evaluation/artifacts"
	"github.com/ISR-Consulting/context-video/internal/evaluation/config"
	"github.com/ISR-Consulting/context-video/internal/telemetry"
	"github.com/ISR-Consulting/context-video/internal/vision"
	visionproviders "github.com/ISR-Consulting/context-video/internal/vision/providers"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// wantSampling is the approved M08 frame sampling per committed experiment.
var wantSampling = map[string]string{"E02": "uniform:2", "E03": "uniform:1", "E04": "uniform:2", "E05": "uniform:4"}

func TestCommittedExperimentsSelectRegisteredPerceptionProviders(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "configs", "experiments", "*.yaml"))
	if err != nil || len(paths) != 5 {
		t.Fatalf("committed configs: %v %v", paths, err)
	}
	audioNames, visionNames := providers.Default().Names(), visionproviders.Default().Names()
	for _, path := range paths {
		cfg, err := config.LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Audio.Enabled != (cfg.ExperimentID != "E02") || cfg.Vision.Enabled != (cfg.ExperimentID != "E01") {
			t.Errorf("%s: modalities audio=%t vision=%t", path, cfg.Audio.Enabled, cfg.Vision.Enabled)
		}
		if cfg.Audio.Enabled && (cfg.Audio.Provider != "whisper-cpp" || !slices.Contains(audioNames, cfg.Audio.Provider)) {
			t.Errorf("%s: audio.provider %q; registered: %v", path, cfg.Audio.Provider, audioNames)
		}
		if cfg.Vision.Enabled {
			if cfg.Vision.Provider != "llama-mtmd" || !slices.Contains(visionNames, cfg.Vision.Provider) {
				t.Errorf("%s: vision.provider %q; registered: %v", path, cfg.Vision.Provider, visionNames)
			}
			if cfg.Vision.Sampling != wantSampling[cfg.ExperimentID] {
				t.Errorf("%s: vision.sampling %q, want %q", path, cfg.Vision.Sampling, wantSampling[cfg.ExperimentID])
			}
			if _, err := vision.ParseSampling(cfg.Vision.Sampling); err != nil {
				t.Errorf("%s: %v", path, err)
			}
		}
	}
}

func readJSONLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []map[string]any
	s := bufio.NewScanner(f)
	s.Buffer(nil, 1<<20)
	for s.Scan() {
		var v map[string]any
		if err := json.Unmarshal(s.Bytes(), &v); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		out = append(out, v)
	}
	return out
}

func readJSON[T any](t *testing.T, path string) T {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestRunMultimodalLiveWritesArtifactsAndMetrics(t *testing.T) {
	output := filepath.Join(t.TempDir(), "runs")
	args := append(multimodalArgs(output, writeMultimodalConfig(t, true, true, "fake")),
		"--pacing", "live", "--speed", "1000", "--cost-per-hour", "2",
		"--context-option", "model=/models/llm.gguf")
	var f fakeRegs
	var stdout, stderr bytes.Buffer
	if err := runWith(args, &stdout, &stderr, f.registries(t)); err != nil {
		t.Fatal(err)
	}
	expDir := filepath.Join(output, "E04")
	result := readJSON[contracts.ExperimentResult](t, filepath.Join(expDir, "poc-golden-v1.0.json"))
	m := result.Metrics
	if m.LatencyP50Ms == nil || m.LatencyP95Ms == nil || m.LatencyP99Ms == nil || *m.SchemaCompliance != 1 ||
		*m.EvidenceTraceability != 1 || m.ContextStability == nil || m.CostPerVideoHour == nil {
		t.Fatalf("metrics %+v", m)
	}
	// Live latency is measured from the window start, so it is at least the
	// window length replayed at 1000x (5 s -> 5 ms).
	if *m.LatencyP50Ms < 5 {
		t.Fatalf("latency p50 %v below the window replay time", *m.LatencyP50Ms)
	}

	v, err := contracts.NewValidator(os.DirFS(filepath.Join("..", "..", "specs")))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []string{"football-live", "visual-vod"} {
		trace := readJSONLines(t, filepath.Join(expDir, "trace", tc+".jsonl"))
		if len(trace) != 2 || trace[0]["pacing"] != "live" || trace[0]["status"] != "ok" || trace[0]["occurredAt"] == nil {
			t.Fatalf("%s trace %v", tc, trace)
		}
		stages := trace[0]["stages"].([]any)
		if len(stages) != 3 {
			t.Fatalf("%s stages %v", tc, stages)
		}
		for name, kind := range map[string]contracts.Kind{
			"audio-observations.jsonl": contracts.KindAudioObservation, "visual-observations.jsonl": contracts.KindVisualObservation,
			"context-events.jsonl": contracts.KindContextEventV1,
		} {
			data, err := os.ReadFile(filepath.Join(expDir, "raw", tc, name))
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
			if len(lines) != 2 {
				t.Fatalf("%s/%s: %d lines", tc, name, len(lines))
			}
			for _, line := range lines {
				if err := v.ValidateJSON(kind, []byte(line)); err != nil {
					t.Fatalf("%s/%s: %v", tc, name, err)
				}
			}
		}
	}
	summary := readJSON[telemetry.Summary](t, filepath.Join(expDir, "summary.json"))
	if summary.Pacing != "live" || summary.Segments != 4 || summary.FailedSegments != 0 || summary.ContextEvents != 4 ||
		summary.ContextAvailabilityLatencyMs == nil || summary.RealTimeFactor == nil {
		t.Fatalf("summary %+v", summary)
	}
	manifest := readJSON[artifacts.RunManifest](t, filepath.Join(expDir, "run-manifest.json"))
	if manifest.Status != "completed" || manifest.Pacing != "live" || *manifest.Speed != 1000 || *manifest.CostPerHour != 2 ||
		manifest.SegmentErrors != "fail" || manifest.Config != "multimodal.yaml" || manifest.Dataset.DatasetID != "poc-golden" ||
		manifest.Options.Context["model"] != (artifacts.OptionValue{File: "llm.gguf"}) {
		t.Fatalf("manifest %+v", manifest)
	}
	raw, err := os.ReadFile(filepath.Join(expDir, "run-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "/models/") {
		t.Fatalf("host path in manifest: %s", raw)
	}
	for _, want := range []string{"artifacts: " + expDir, "pacing: live 1000x", "segments: 4 (failed 0)", "real-time factor:"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
	if !strings.Contains(stderr.String(), "E04 football-live segment 2/2") {
		t.Errorf("progress missing:\n%s", stderr.String())
	}

	// A second run into the same output is refused before any provider call.
	var g fakeRegs
	err = runWith(args, &stdout, &stderr, g.registries(t))
	if !errors.Is(err, artifacts.ErrExists) || len(g.audioRequests) != 0 {
		t.Fatalf("rerun: %v (audio calls %d)", err, len(g.audioRequests))
	}
}

// failingOnceRegistry registers a "fake" reasoner that fails on the window
// starting at failStartMs and otherwise cites all of the group's evidence.
func failingOnceRegistry(t *testing.T, failStartMs int64) *contextcore.Registry {
	t.Helper()
	var opts contextcore.Options
	var groups []contextcore.EvidenceGroup
	inner, err := fakeContextRegistry(t, &opts, &groups).New("fake", nil)
	if err != nil {
		t.Fatal(err)
	}
	r := contextcore.NewRegistry()
	if err := r.Register("fake", func(contextcore.Options) (contextcore.Reasoner, error) {
		return contextcore.ReasonerFunc(func(ctx context.Context, g contextcore.EvidenceGroup) ([]contextcore.Candidate, error) {
			if g.Window.StartMs == failStartMs {
				return nil, errors.New("malformed answer")
			}
			return inner.Reason(ctx, g)
		}), nil
	}); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRunMultimodalRecordedSegmentErrors(t *testing.T) {
	output := filepath.Join(t.TempDir(), "runs")
	args := append(multimodalArgs(output, writeMultimodalConfig(t, true, true, "fake")), "--on-segment-error", "record")
	var f fakeRegs
	regs := f.registries(t)
	regs.context = failingOnceRegistry(t, 5000)
	var stdout, stderr bytes.Buffer
	if err := runWith(args, &stdout, &stderr, regs); err != nil {
		t.Fatal(err)
	}
	expDir := filepath.Join(output, "E04")
	summary := readJSON[telemetry.Summary](t, filepath.Join(expDir, "summary.json"))
	if summary.Segments != 4 || summary.FailedSegments != 2 || summary.FailedByStage["reason"] != 2 || summary.ContextEvents != 2 ||
		summary.AudioObservations != 4 {
		t.Fatalf("summary %+v", summary)
	}
	trace := readJSONLines(t, filepath.Join(expDir, "trace", "football-live.jsonl"))
	if trace[1]["status"] != "failed" || trace[1]["failedStage"] != "reason" || !strings.Contains(trace[1]["error"].(string), "malformed answer") {
		t.Fatalf("trace %v", trace)
	}
	result := readJSON[contracts.ExperimentResult](t, filepath.Join(expDir, "poc-golden-v1.0.json"))
	if result.Metrics.LatencyP50Ms != nil || *result.Metrics.EvidenceTraceability != 1 {
		t.Fatalf("instant metrics %+v", result.Metrics)
	}
	if !strings.Contains(stdout.String(), "segments: 4 (failed 2)") || !strings.Contains(stdout.String(), "pacing: instant") {
		t.Fatalf("stdout:\n%s", stdout.String())
	}
}

func TestRunMultimodalFailedRunKeepsTraceAndManifest(t *testing.T) {
	output := filepath.Join(t.TempDir(), "runs")
	args := multimodalArgs(output, writeMultimodalConfig(t, true, true, "fake"))
	var f fakeRegs
	regs := f.registries(t)
	regs.context = failingOnceRegistry(t, 5000)
	var stdout, stderr bytes.Buffer
	err := runWith(args, &stdout, &stderr, regs)
	if err == nil || !strings.Contains(err.Error(), "malformed answer") {
		t.Fatalf("err %v", err)
	}
	expDir := filepath.Join(output, "E04")
	if _, err := os.Stat(filepath.Join(expDir, "poc-golden-v1.0.json")); !os.IsNotExist(err) {
		t.Fatalf("result written for a failed run: %v", err)
	}
	trace := readJSONLines(t, filepath.Join(expDir, "trace", "football-live.jsonl"))
	if len(trace) != 2 || trace[1]["status"] != "failed" {
		t.Fatalf("trace %v", trace)
	}
	manifest := readJSON[artifacts.RunManifest](t, filepath.Join(expDir, "run-manifest.json"))
	if manifest.Status != "failed" || !strings.Contains(manifest.Error, "malformed answer") {
		t.Fatalf("manifest %+v", manifest)
	}
}

func TestRunMultimodalInstantRunsAreReproducible(t *testing.T) {
	config := writeMultimodalConfig(t, true, true, "fake")
	var files [2][]byte
	for i := range files {
		output := filepath.Join(t.TempDir(), "runs")
		var f fakeRegs
		var stdout, stderr bytes.Buffer
		if err := runWith(multimodalArgs(output, config), &stdout, &stderr, f.registries(t)); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"audio-observations.jsonl", "visual-observations.jsonl", "context-events.jsonl"} {
			data, err := os.ReadFile(filepath.Join(output, "E04", "raw", "football-live", name))
			if err != nil {
				t.Fatal(err)
			}
			files[i] = append(files[i], data...)
		}
	}
	if !bytes.Equal(files[0], files[1]) {
		t.Fatal("raw contract output differs between identical instant runs")
	}
}

func TestM08FlagValidation(t *testing.T) {
	mm := func(t *testing.T, output string) []string {
		return multimodalArgs(output, writeMultimodalConfig(t, true, true, "fake"))
	}
	for name, tc := range map[string]struct {
		args func(t *testing.T, output string) []string
		want string
	}{
		"pacing on audio pipeline": {func(t *testing.T, o string) []string {
			return append(withFlag(withFlag(baseArgs(o), "--pipeline", "audio"), "--config", writeAudioConfig(t, "fake")), "--pacing", "live")
		}, "--pacing is only valid with --pipeline multimodal"},
		"cost on validation-only": {func(t *testing.T, o string) []string { return append(baseArgs(o), "--cost-per-hour", "1") },
			"--cost-per-hour is only valid with --pipeline multimodal"},
		"unknown pacing":  {func(t *testing.T, o string) []string { return append(mm(t, o), "--pacing", "vod") }, `unknown --pacing "vod"`},
		"speed instant":   {func(t *testing.T, o string) []string { return append(mm(t, o), "--speed", "2") }, "--speed is only valid with --pacing live"},
		"zero speed":      {func(t *testing.T, o string) []string { return append(mm(t, o), "--pacing", "live", "--speed", "0") }, "--speed:"},
		"unknown policy":  {func(t *testing.T, o string) []string { return append(mm(t, o), "--on-segment-error", "skip") }, `unknown --on-segment-error "skip"`},
		"negative price":  {func(t *testing.T, o string) []string { return append(mm(t, o), "--cost-per-hour", "-1") }, "--cost-per-hour must be"},
		"not-a-number":    {func(t *testing.T, o string) []string { return append(mm(t, o), "--cost-per-hour", "NaN") }, "--cost-per-hour must be"},
		"infinite speed":  {func(t *testing.T, o string) []string { return append(mm(t, o), "--pacing", "live", "--speed", "+Inf") }, "--speed:"},
		"record on audio": {func(t *testing.T, o string) []string { return append(baseArgs(o), "--on-segment-error", "record") }, "--on-segment-error is only valid"},
	} {
		t.Run(name, func(t *testing.T) {
			var f fakeRegs
			var stdout, stderr bytes.Buffer
			output := filepath.Join(t.TempDir(), "runs")
			err := runWith(tc.args(t, output), &stdout, &stderr, f.registries(t))
			var usage usageError
			if !errors.As(err, &usage) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err %v, want usage error containing %q", err, tc.want)
			}
			if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
				t.Fatalf("output created for a usage error")
			}
		})
	}
}
