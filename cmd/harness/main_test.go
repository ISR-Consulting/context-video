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

	"github.com/ISR-Consulting/context-video/internal/audio"
	"github.com/ISR-Consulting/context-video/internal/evaluation/config"
	"github.com/ISR-Consulting/context-video/internal/evaluation/harness"
)

var (
	configFile       = filepath.Join("..", "..", "configs", "experiments", "multimodal-5s.yaml")
	audioOnlyConfig  = filepath.Join("..", "..", "configs", "experiments", "audio-only-5s.yaml")
	visionOnlyConfig = filepath.Join("..", "..", "configs", "experiments", "vision-only-5s.yaml")
	datasetRoot      = filepath.Join("..", "..", "internal", "evaluation", "dataset", "testdata", "valid")
	specsRoot        = filepath.Join("..", "..", "specs")
)

const manifest = "manifests/poc-golden-v1.0.json"

func baseArgs(output string) []string {
	return []string{
		"--config", configFile,
		"--manifest", manifest,
		"--dataset-root", datasetRoot,
		"--specs-root", specsRoot,
		"--output", output,
		"--pipeline", "validation-only",
	}
}

func withFlag(args []string, name, value string) []string {
	out := append([]string(nil), args...)
	for i := range out {
		if out[i] == name {
			out[i+1] = value
			return out
		}
	}
	return append(out, name, value)
}

func TestRunValidationOnlyHappyPath(t *testing.T) {
	output := filepath.Join(t.TempDir(), "results")
	var stdout, stderr bytes.Buffer
	if err := run(baseArgs(output), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(output, "E04", "poc-golden-v1.0.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"experimentId": "E04"`) || !strings.Contains(string(data), `"metrics": {}`) {
		t.Fatalf("result:\n%s", data)
	}
	for _, want := range []string{
		"result: " + path,
		"experiment: E04",
		"pipeline: validation-only",
		"dataset: poc-golden v1.0",
		"test cases processed: 2",
		"elapsed: ",
		"outputs: audio observations=0 visual observations=0 context events=0",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr: %s", stderr.String())
	}
}

func TestRunFailures(t *testing.T) {
	invalidConfig := filepath.Join(t.TempDir(), "invalid.yaml")
	if err := os.WriteFile(invalidConfig, []byte("experiment:\n  id: E99\n  unknown: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	whisperConfig := writeAudioConfig(t, "whisper-cpp")
	cases := []struct {
		name  string
		args  func(output string) []string
		usage bool
		check func(t *testing.T, err error)
		want  string
	}{
		{
			name:  "missing required flags",
			args:  func(string) []string { return []string{"--pipeline", "validation-only"} },
			usage: true, want: "missing required flags: --config, --manifest, --output",
		},
		{
			name:  "unknown flag",
			args:  func(output string) []string { return append(baseArgs(output), "--provider", "x") },
			usage: true, want: "flag provided but not defined: -provider",
		},
		{
			name:  "positional arguments",
			args:  func(output string) []string { return append(baseArgs(output), "extra") },
			usage: true, want: "unexpected arguments: extra",
		},
		{
			name:  "unknown pipeline",
			args:  func(output string) []string { return withFlag(baseArgs(output), "--pipeline", "gpt") },
			usage: true, want: `unknown pipeline "gpt"; available: audio, validation-only, vision`,
		},
		{
			name: "audio option without audio pipeline",
			args: func(output string) []string {
				return append(baseArgs(output), "--audio-option", "model=m.bin")
			},
			usage: true, want: "--audio-option is only valid with --pipeline audio",
		},
		{
			name: "malformed audio option",
			args: func(output string) []string {
				return append(withFlag(baseArgs(output), "--pipeline", "audio"), "--audio-option", "model")
			},
			usage: true, want: `want key=value, got "model"`,
		},
		{
			name: "duplicate audio option",
			args: func(output string) []string {
				return append(withFlag(baseArgs(output), "--pipeline", "audio"),
					"--audio-option", "model=a", "--audio-option", "model=b")
			},
			usage: true, want: `duplicate option "model"`,
		},
		{
			name: "audio pipeline with TBD provider",
			args: func(output string) []string {
				return withFlag(withFlag(baseArgs(output), "--pipeline", "audio"), "--config", audioOnlyConfig)
			},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, audio.ErrUnknownProvider) {
					t.Fatalf("expected unknown provider: %v", err)
				}
			},
			want: `unknown audio provider "TBD"; available: whisper-cpp`,
		},
		{
			name: "audio pipeline with audio disabled",
			args: func(output string) []string {
				return withFlag(withFlag(baseArgs(output), "--pipeline", "audio"), "--config", visionOnlyConfig)
			},
			want: `pipeline "audio" requires audio.enabled: true in experiment E02`,
		},
		{
			name: "whisper-cpp without model",
			args: func(output string) []string {
				return withFlag(withFlag(baseArgs(output), "--pipeline", "audio"), "--config", whisperConfig)
			},
			want: `audio provider "whisper-cpp": option "model"`,
		},
		{
			name: "invalid config",
			args: func(output string) []string { return withFlag(baseArgs(output), "--config", invalidConfig) },
			check: func(t *testing.T, err error) {
				var cfgErr *config.Error
				if !errors.As(err, &cfgErr) || cfgErr.Stage != config.StageDecode {
					t.Fatalf("expected config decode error: %v", err)
				}
			},
			want: "field unknown not found",
		},
		{
			name: "missing config",
			args: func(output string) []string { return withFlag(baseArgs(output), "--config", "missing.yaml") },
			want: "config read: missing.yaml",
		},
		{
			name: "invalid dataset",
			args: func(output string) []string {
				return withFlag(baseArgs(output), "--manifest", "manifests/missing.json")
			},
			check: func(t *testing.T, err error) {
				var harnessErr *harness.Error
				if !errors.As(err, &harnessErr) || harnessErr.Stage != harness.StageDataset {
					t.Fatalf("expected dataset error: %v", err)
				}
			},
			want: "read manifest",
		},
		{
			name: "missing specs",
			args: func(output string) []string { return withFlag(baseArgs(output), "--specs-root", t.TempDir()) },
			want: "harness specs",
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

// writeAudioConfig writes the committed audio-only E01 configuration with
// audio.provider replaced, keeping the YAML shape unchanged.
func writeAudioConfig(t *testing.T, provider string) string {
	t.Helper()
	data, err := os.ReadFile(audioOnlyConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "provider: TBD\nvision") {
		t.Fatalf("unexpected E01 config layout:\n%s", data)
	}
	path := filepath.Join(t.TempDir(), "e01.yaml")
	patched := strings.Replace(string(data), "provider: TBD\nvision", "provider: "+provider+"\nvision", 1)
	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// fakeRegistry registers a "fake" provider that records its options and the
// requests it receives.
func fakeRegistry(t *testing.T, opts *audio.Options, requests *[]audio.Request, fail error) *audio.Registry {
	t.Helper()
	r := audio.NewRegistry()
	err := r.Register("fake", func(o audio.Options) (audio.Transcriber, error) {
		*opts = o
		return audio.TranscriberFunc(func(_ context.Context, req audio.Request) (audio.Transcription, error) {
			*requests = append(*requests, req)
			if fail != nil && req.Source.Kind == audio.SourceControlledSource {
				return audio.Transcription{}, fail
			}
			return audio.Transcription{Text: "fala", Language: "pt", Provider: "fake"}, nil
		}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRunAudioPipelineWithFakeProvider(t *testing.T) {
	output := filepath.Join(t.TempDir(), "results")
	args := withFlag(withFlag(baseArgs(output), "--pipeline", "audio"), "--config", writeAudioConfig(t, "fake"))
	args = append(args, "--audio-option", "model=/models/m.bin", "--audio-option", "language=pt")
	var opts audio.Options
	var requests []audio.Request
	var stdout, stderr bytes.Buffer
	if err := runWith(args, &stdout, &stderr, registries{audio: fakeRegistry(t, &opts, &requests, nil)}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(opts, audio.Options{"model": "/models/m.bin", "language": "pt"}) {
		t.Fatalf("options: %v", opts)
	}
	if len(requests) != 4 {
		t.Fatalf("requests: %d", len(requests))
	}
	if want := filepath.Join(datasetRoot, "media", "football-live.fixture"); requests[0].Source.Path != want {
		t.Fatalf("media path %q want %q", requests[0].Source.Path, want)
	}
	data, err := os.ReadFile(filepath.Join(output, "E01", "poc-golden-v1.0.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"provider": "fake"`) || !strings.Contains(string(data), `"metrics": {}`) {
		t.Fatalf("result:\n%s", data)
	}
	for _, want := range []string{
		"pipeline: audio",
		"test cases processed: 2",
		"outputs: audio observations=4 visual observations=0 context events=0",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunAudioPipelineFailureWritesNothing(t *testing.T) {
	output := filepath.Join(t.TempDir(), "results")
	args := withFlag(withFlag(baseArgs(output), "--pipeline", "audio"), "--config", writeAudioConfig(t, "fake"))
	var opts audio.Options
	var requests []audio.Request
	var stdout, stderr bytes.Buffer
	err := runWith(args, &stdout, &stderr, registries{audio: fakeRegistry(t, &opts, &requests, audio.ErrUnsupportedSource)})
	var harnessErr *harness.Error
	if !errors.As(err, &harnessErr) || harnessErr.Stage != harness.StagePipeline || harnessErr.TestCaseID != "visual-vod" {
		t.Fatalf("err: %v", err)
	}
	if !errors.Is(err, audio.ErrUnsupportedSource) {
		t.Fatalf("cause lost: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout: %s", stdout.String())
	}
	if _, statErr := os.Stat(output); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("output written on failure: %v", statErr)
	}
}

func TestRunRefusesToOverwriteResult(t *testing.T) {
	output := t.TempDir()
	var stdout, stderr bytes.Buffer
	if err := run(baseArgs(output), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	err := run(baseArgs(output), &stdout, &stderr)
	if !errors.Is(err, harness.ErrResultExists) {
		t.Fatalf("expected collision, got %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout on collision: %s", stdout.String())
	}
}

func TestHelpIsNotAnError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-h"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "usage: harness") {
		t.Fatalf("help: %s", stderr.String())
	}
}
