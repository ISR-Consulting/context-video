package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ISR-Consulting/context-video/internal/evaluation/config"
	"github.com/ISR-Consulting/context-video/internal/evaluation/harness"
)

var (
	configFile  = filepath.Join("..", "..", "configs", "experiments", "multimodal-5s.yaml")
	datasetRoot = filepath.Join("..", "..", "internal", "evaluation", "dataset", "testdata", "valid")
	specsRoot   = filepath.Join("..", "..", "specs")
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
			usage: true, want: `unknown pipeline "gpt"; available: validation-only`,
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
