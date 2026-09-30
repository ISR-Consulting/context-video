package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ISR-Consulting/context-video/internal/evaluation/harness"
	"github.com/ISR-Consulting/context-video/internal/vision"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// writeVisionConfig writes the committed vision-only E02 configuration with
// vision.provider and vision.sampling replaced, keeping the YAML shape.
func writeVisionConfig(t *testing.T, provider, sampling string) string {
	t.Helper()
	data, err := os.ReadFile(visionOnlyConfig)
	if err != nil {
		t.Fatal(err)
	}
	const committed = "vision:\n  enabled: true\n  provider: llama-mtmd\n  sampling: uniform:2\n"
	if !strings.Contains(string(data), committed) {
		t.Fatalf("unexpected E02 config layout:\n%s", data)
	}
	patched := strings.Replace(string(data), committed,
		"vision:\n  enabled: true\n  provider: "+provider+"\n  sampling: "+sampling+"\n", 1)
	path := filepath.Join(t.TempDir(), "e02.yaml")
	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// fakeVisionRegistry registers a "fake" provider that records its options and
// the requests it receives.
func fakeVisionRegistry(t *testing.T, opts *vision.Options, requests *[]vision.Request, fail error) *vision.Registry {
	t.Helper()
	r := vision.NewRegistry()
	err := r.Register("fake", func(o vision.Options) (vision.Analyzer, error) {
		*opts = o
		return vision.AnalyzerFunc(func(_ context.Context, req vision.Request) (vision.Analysis, error) {
			*requests = append(*requests, req)
			if fail != nil && req.Source.Kind == vision.SourceControlledSource {
				return vision.Analysis{}, fail
			}
			a := vision.Analysis{Provider: "fake"}
			for _, ts := range req.FrameTimesMs {
				confidence := 0.5
				a.Frames = append(a.Frames, vision.Frame{TimestampMs: ts, Detections: []vision.Detection{
					{Type: contracts.VisualDetectionScene, Value: "stadium", Confidence: &confidence},
				}})
			}
			return a, nil
		}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRunVisionPipelineWithFakeProvider(t *testing.T) {
	output := filepath.Join(t.TempDir(), "results")
	args := withFlag(withFlag(baseArgs(output), "--pipeline", "vision"), "--config", writeVisionConfig(t, "fake", "uniform:2"))
	args = append(args, "--vision-option", "model=/models/vlm.gguf", "--vision-option", "mmproj=/models/mmproj.gguf")
	var opts vision.Options
	var requests []vision.Request
	var stdout, stderr bytes.Buffer
	if err := runWith(args, &stdout, &stderr, registries{vision: fakeVisionRegistry(t, &opts, &requests, nil)}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(opts, vision.Options{"model": "/models/vlm.gguf", "mmproj": "/models/mmproj.gguf"}) {
		t.Fatalf("options: %v", opts)
	}
	if len(requests) != 4 {
		t.Fatalf("requests: %d", len(requests))
	}
	if want := filepath.Join(datasetRoot, "media", "football-live.fixture"); requests[0].Source.Path != want {
		t.Fatalf("media path %q want %q", requests[0].Source.Path, want)
	}
	if !reflect.DeepEqual(requests[0].FrameTimesMs, []int64{1250, 3750}) {
		t.Fatalf("frames: %v", requests[0].FrameTimesMs)
	}
	data, err := os.ReadFile(filepath.Join(output, "E02", "poc-golden-v1.0.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"provider": "fake"`, `"sampling": "uniform:2"`, `"metrics": {}`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("result missing %q:\n%s", want, data)
		}
	}
	for _, want := range []string{
		"pipeline: vision",
		"test cases processed: 2",
		"outputs: audio observations=0 visual observations=4 context events=0",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunVisionPipelineFailureWritesNothing(t *testing.T) {
	output := filepath.Join(t.TempDir(), "results")
	args := withFlag(withFlag(baseArgs(output), "--pipeline", "vision"), "--config", writeVisionConfig(t, "fake", "uniform:1"))
	var opts vision.Options
	var requests []vision.Request
	var stdout, stderr bytes.Buffer
	err := runWith(args, &stdout, &stderr, registries{vision: fakeVisionRegistry(t, &opts, &requests, vision.ErrUnsupportedSource)})
	var harnessErr *harness.Error
	if !errors.As(err, &harnessErr) || harnessErr.Stage != harness.StagePipeline || harnessErr.TestCaseID != "visual-vod" {
		t.Fatalf("err: %v", err)
	}
	if !errors.Is(err, vision.ErrUnsupportedSource) {
		t.Fatalf("cause lost: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout: %s", stdout.String())
	}
	if _, statErr := os.Stat(output); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("output written on failure: %v", statErr)
	}
}

func TestRunVisionFailures(t *testing.T) {
	visionArgs := func(output, config string) []string {
		return withFlag(withFlag(baseArgs(output), "--pipeline", "vision"), "--config", config)
	}
	cases := []struct {
		name  string
		args  func(output string) []string
		usage bool
		check func(t *testing.T, err error)
		want  string
	}{
		{
			name:  "vision option without vision pipeline",
			args:  func(output string) []string { return append(baseArgs(output), "--vision-option", "model=m.gguf") },
			usage: true, want: "--vision-option is only valid with --pipeline vision",
		},
		{
			name: "audio option with vision pipeline",
			args: func(output string) []string {
				return append(visionArgs(output, visionOnlyConfig), "--audio-option", "model=m.bin")
			},
			usage: true, want: "--audio-option is only valid with --pipeline audio",
		},
		{
			name: "malformed vision option",
			args: func(output string) []string {
				return append(visionArgs(output, visionOnlyConfig), "--vision-option", "mmproj")
			},
			usage: true, want: `want key=value, got "mmproj"`,
		},
		{
			name: "duplicate vision option",
			args: func(output string) []string {
				return append(visionArgs(output, visionOnlyConfig), "--vision-option", "model=a", "--vision-option", "model=b")
			},
			usage: true, want: `duplicate option "model"`,
		},
		{
			name: "committed E02 configuration without a model",
			args: func(output string) []string { return visionArgs(output, visionOnlyConfig) },
			want: `vision provider "llama-mtmd": option "model" (path to a GGUF vision-language model) is required`,
		},
		{
			name: "TBD provider",
			args: func(output string) []string { return visionArgs(output, writeVisionConfig(t, "TBD", "uniform:2")) },
			check: func(t *testing.T, err error) {
				if !errors.Is(err, vision.ErrUnknownProvider) {
					t.Fatalf("expected unknown provider: %v", err)
				}
			},
			want: `unknown vision provider "TBD"; available: llama-mtmd`,
		},
		{
			name: "vision pipeline with vision disabled",
			args: func(output string) []string { return visionArgs(output, audioOnlyConfig) },
			want: `pipeline "vision" requires vision.enabled: true in experiment E01`,
		},
		{
			name: "llama-mtmd without model",
			args: func(output string) []string {
				return visionArgs(output, writeVisionConfig(t, "llama-mtmd", "uniform:2"))
			},
			want: `vision provider "llama-mtmd": option "model"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "results")
			var stdout, stderr bytes.Buffer
			err := run(tc.args(output), &stdout, &stderr)
			if err == nil {
				t.Fatal("expected error")
			}
			var usage usageError
			if errors.As(err, &usage) != tc.usage {
				t.Fatalf("usage error = %v, want %v: %v", !tc.usage, tc.usage, err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err, tc.want)
			}
			if tc.check != nil {
				tc.check(t, err)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout on failure: %s", stdout.String())
			}
			if _, statErr := os.Stat(output); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("output written on failure: %v", statErr)
			}
		})
	}
}
