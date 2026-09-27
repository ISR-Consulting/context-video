package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ISR-Consulting/context-video/internal/evaluation/config"
	"github.com/ISR-Consulting/context-video/internal/evaluation/dataset"
	"github.com/ISR-Consulting/context-video/internal/media"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

var (
	configs     = filepath.Join("..", "..", "configs", "experiments")
	configFile  = filepath.Join(configs, "multimodal-5s.yaml")
	datasetRoot = filepath.Join("..", "..", "internal", "evaluation", "dataset", "testdata", "valid")
	specsRoot   = filepath.Join("..", "..", "specs")
)

const manifest = "manifests/poc-golden-v1.0.json"

const firstLine = `{"segmentId":"live-football-001:0-5000","content":{"contentId":"live-football-001","contentType":"LIVE"},"window":{"startMs":0,"endMs":5000},"sourceUri":"media/football-live.fixture"}`

func baseArgs(extra ...string) []string {
	return append([]string{
		"--config", configFile,
		"--manifest", manifest,
		"--dataset-root", datasetRoot,
		"--specs-root", specsRoot,
	}, extra...)
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

func runOK(t *testing.T, args []string) (string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}
	return stdout.String(), stderr.String()
}

func lines(t *testing.T, stdout string) []string {
	t.Helper()
	if !strings.HasSuffix(stdout, "\n") {
		t.Fatalf("stdout does not end in a newline: %q", stdout)
	}
	return strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
}

func checkSegments(t *testing.T, out []string) []contracts.MediaSegment {
	t.Helper()
	validator, err := contracts.NewValidator(os.DirFS(specsRoot))
	if err != nil {
		t.Fatal(err)
	}
	segments := make([]contracts.MediaSegment, len(out))
	for i, line := range out {
		if err := validator.ValidateJSON(contracts.KindMediaSegment, []byte(line)); err != nil {
			t.Fatalf("line %d schema: %v\n%s", i, err, line)
		}
		if err := json.Unmarshal([]byte(line), &segments[i]); err != nil {
			t.Fatal(err)
		}
		if err := contracts.Validate(segments[i]); err != nil {
			t.Fatalf("line %d domain: %v", i, err)
		}
	}
	return segments
}

func TestRunInstantFixture(t *testing.T) {
	stdout, stderr := runOK(t, baseArgs("--instant"))
	out := lines(t, stdout)
	if len(out) != 4 {
		t.Fatalf("got %d lines, want 4:\n%s", len(out), stdout)
	}
	if out[0] != firstLine {
		t.Fatalf("first line:\n%s\nwant:\n%s", out[0], firstLine)
	}
	segments := checkSegments(t, out)
	if last := segments[3]; last.SegmentID != "vod-visual-001:5000-8000" || last.Content.ContentType != contracts.ContentTypeVOD ||
		*last.SourceURI != "controlled://approved/visual-vod-001" {
		t.Fatalf("last segment = %+v", last)
	}
	for _, want := range []string{
		"test case football-live: content live-football-001 (LIVE): 2 segments, window 5s, pacing instant",
		"test case visual-vod: content vod-visual-001 (VOD): 2 segments, window 5s, pacing instant",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q:\n%s", want, stderr)
		}
	}

	again, _ := runOK(t, baseArgs("--instant"))
	if again != stdout {
		t.Fatalf("output differs across runs:\n%s\n%s", stdout, again)
	}
}

func TestRunSingleTestCase(t *testing.T) {
	stdout, stderr := runOK(t, baseArgs("--instant", "--test-case", "football-live"))
	out := lines(t, stdout)
	if len(out) != 2 || out[0] != firstLine {
		t.Fatalf("stdout:\n%s", stdout)
	}
	if strings.Contains(stderr, "visual-vod") {
		t.Fatalf("unselected test case replayed:\n%s", stderr)
	}
}

func TestRunTwoSecondWindows(t *testing.T) {
	stdout, _ := runOK(t, withFlag(baseArgs("--instant"), "--config", filepath.Join(configs, "multimodal-2s.yaml")))
	segments := checkSegments(t, lines(t, stdout))
	counts := map[string]int{}
	for _, segment := range segments {
		counts[segment.Content.ContentID]++
	}
	if len(segments) != 9 || counts["live-football-001"] != 5 || counts["vod-visual-001"] != 4 {
		t.Fatalf("counts = %v (total %d), want 5 + 4", counts, len(segments))
	}
	if last := segments[8].Window; last.StartMs != 6000 || last.EndMs != 8000 {
		t.Fatalf("last window = %+v", last)
	}
}

func TestRunPaced(t *testing.T) {
	stdout, stderr := runOK(t, baseArgs("--speed", "1000"))
	instant, _ := runOK(t, baseArgs("--instant"))
	if stdout != instant {
		t.Fatalf("paced output differs from instant:\n%s\n%s", stdout, instant)
	}
	if !strings.Contains(stderr, "pacing speed 1000x") {
		t.Fatalf("stderr: %s", stderr)
	}
}

func TestRunUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing required flags", []string{"--instant"}, "missing required flags: --config, --manifest"},
		{"unknown flag", baseArgs("--provider", "x"), "flag provided but not defined: -provider"},
		{"positional arguments", baseArgs("extra"), "unexpected arguments: extra"},
		{"speed and instant", baseArgs("--speed", "2", "--instant"), "--speed and --instant are mutually exclusive"},
		{"zero speed", baseArgs("--speed", "0"), "invalid pacing"},
		{"negative speed", baseArgs("--speed", "-1"), "invalid pacing"},
		{"NaN speed", baseArgs("--speed", "NaN"), "invalid pacing"},
		{"infinite speed", baseArgs("--speed", "Inf"), "invalid pacing"},
		{"malformed speed", baseArgs("--speed", "fast"), "invalid value"},
		{"unknown test case", baseArgs("--instant", "--test-case", "nope"), `unknown test case "nope"; available: football-live, visual-vod`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(tc.args, &stdout, &stderr)
			var usage usageError
			if !errors.As(err, &usage) || exitCode(err) != 2 {
				t.Fatalf("expected usage error with exit 2, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err, tc.want)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout on usage error: %s", stdout.String())
			}
		})
	}
}

func TestRunRuntimeErrors(t *testing.T) {
	invalidConfig := filepath.Join(t.TempDir(), "invalid.yaml")
	if err := os.WriteFile(invalidConfig, []byte("experiment:\n  id: E99\n  unknown: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tampered := t.TempDir()
	if err := os.CopyFS(tampered, os.DirFS(datasetRoot)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tampered, "media", "football-live.fixture"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		args  []string
		want  string
		check func(t *testing.T, err error)
	}{
		{
			name: "invalid config",
			args: withFlag(baseArgs("--instant"), "--config", invalidConfig),
			want: "field unknown not found",
			check: func(t *testing.T, err error) {
				var cfgErr *config.Error
				if !errors.As(err, &cfgErr) || cfgErr.Stage != config.StageDecode {
					t.Fatalf("expected config decode error: %v", err)
				}
			},
		},
		{
			name: "missing config",
			args: withFlag(baseArgs("--instant"), "--config", "missing.yaml"),
			want: "config read: missing.yaml",
		},
		{
			name: "digest mismatch",
			args: withFlag(baseArgs("--instant"), "--dataset-root", tampered),
			want: "digest mismatch",
			check: func(t *testing.T, err error) {
				var dsErr *dataset.Error
				if !errors.As(err, &dsErr) || dsErr.Layer != dataset.LayerReference {
					t.Fatalf("expected dataset reference error: %v", err)
				}
			},
		},
		{
			name: "missing manifest",
			args: withFlag(baseArgs("--instant"), "--manifest", "manifests/missing.json"),
			want: "read manifest",
		},
		{
			name: "missing specs",
			args: withFlag(baseArgs("--instant"), "--specs-root", t.TempDir()),
			want: "load specs",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(tc.args, &stdout, &stderr)
			if err == nil {
				t.Fatal("expected error")
			}
			var usage usageError
			if errors.As(err, &usage) || exitCode(err) != 1 {
				t.Fatalf("runtime failure must exit 1, got %d: %v", exitCode(err), err)
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
		})
	}
}

func TestCancellationExitsOne(t *testing.T) {
	err := fmt.Errorf("test case football-live: %w", &media.Error{Stage: media.StageCancelled, Err: context.Canceled})
	if code := exitCode(err); code != 1 {
		t.Fatalf("exit code %d, want 1", code)
	}
}

func TestHelpIsNotAnError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"-h"}, &stdout, &stderr)
	if err != nil || exitCode(err) != 0 {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "usage: live-simulator") || !strings.Contains(stderr.String(), "-instant") {
		t.Fatalf("help: %s", stderr.String())
	}
}
