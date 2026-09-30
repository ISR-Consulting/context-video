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
	contextcore "github.com/ISR-Consulting/context-video/internal/context"
	"github.com/ISR-Consulting/context-video/internal/vision"
)

// writeMultimodalConfig writes the committed E04 configuration with every TBD
// provider and sampling value replaced, keeping the YAML shape. Passing
// audio=false or vision=false writes the committed E02 or E01 shape instead.
func writeMultimodalConfig(t *testing.T, withAudio, withVision bool, fusion string) string {
	t.Helper()
	source := configFile
	switch {
	case !withAudio:
		source = visionOnlyConfig
	case !withVision:
		source = audioOnlyConfig
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, r := range []struct{ old, new string }{
		{"sampling: TBD", "sampling: uniform:2"},
		{"fusion:\n  provider: TBD", "fusion:\n  provider: " + fusion},
		{"provider: TBD", "provider: fake"},
		{"provider: TBD", "provider: fake"},
	} {
		text = strings.Replace(text, r.old, r.new, 1)
	}
	if strings.Contains(strings.Replace(text, "fusion:\n  provider: TBD", "", 1), "TBD") {
		t.Fatalf("unexpected config layout:\n%s", text)
	}
	path := filepath.Join(t.TempDir(), "multimodal.yaml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// fakeContextRegistry registers a "fake" reasoner that records its options and
// concludes one topic per group, citing all of the group's evidence.
func fakeContextRegistry(t *testing.T, opts *contextcore.Options, groups *[]contextcore.EvidenceGroup) *contextcore.Registry {
	t.Helper()
	r := contextcore.NewRegistry()
	err := r.Register("fake", func(o contextcore.Options) (contextcore.Reasoner, error) {
		*opts = o
		return contextcore.ReasonerFunc(func(_ context.Context, g contextcore.EvidenceGroup) ([]contextcore.Candidate, error) {
			*groups = append(*groups, g)
			var refs []contextcore.EvidenceRef
			for _, e := range g.Evidence {
				if e.Facet != contextcore.FacetDetection {
					refs = append(refs, e.Ref())
				}
			}
			if len(refs) == 0 {
				refs = []contextcore.EvidenceRef{g.Evidence[0].Ref()}
			}
			confidence := 0.6
			return []contextcore.Candidate{{
				Topics:     []contextcore.Value{{Value: "football", Confidence: &confidence}},
				Confidence: &confidence,
				Evidence:   refs,
				Reasoning:  contextcore.ReasoningMetadata{Provider: "fake"},
			}}, nil
		}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

type fakeRegs struct {
	audioOpts      audio.Options
	audioRequests  []audio.Request
	visionOpts     vision.Options
	visionRequests []vision.Request
	contextOpts    contextcore.Options
	groups         []contextcore.EvidenceGroup
}

func (f *fakeRegs) registries(t *testing.T) registries {
	return registries{
		audio:   fakeRegistry(t, &f.audioOpts, &f.audioRequests, nil),
		vision:  fakeVisionRegistry(t, &f.visionOpts, &f.visionRequests, nil),
		context: fakeContextRegistry(t, &f.contextOpts, &f.groups),
	}
}

func multimodalArgs(output, config string) []string {
	return withFlag(withFlag(baseArgs(output), "--pipeline", "multimodal"), "--config", config)
}

func TestRunMultimodalPipelineWithFakeProviders(t *testing.T) {
	output := filepath.Join(t.TempDir(), "results")
	args := multimodalArgs(output, writeMultimodalConfig(t, true, true, "fake"))
	args = append(args,
		"--audio-option", "model=/models/stt.bin",
		"--vision-option", "model=/models/vlm.gguf",
		"--context-option", "model=/models/llm.gguf", "--context-option", "threads=4")
	var f fakeRegs
	var stdout, stderr bytes.Buffer
	if err := runWith(args, &stdout, &stderr, f.registries(t)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.audioOpts, audio.Options{"model": "/models/stt.bin"}) ||
		!reflect.DeepEqual(f.visionOpts, vision.Options{"model": "/models/vlm.gguf"}) ||
		!reflect.DeepEqual(f.contextOpts, contextcore.Options{"model": "/models/llm.gguf", "threads": "4"}) {
		t.Fatalf("options: %v %v %v", f.audioOpts, f.visionOpts, f.contextOpts)
	}
	if len(f.audioRequests) != 4 || len(f.visionRequests) != 4 || len(f.groups) != 4 {
		t.Fatalf("calls: audio %d vision %d reason %d", len(f.audioRequests), len(f.visionRequests), len(f.groups))
	}
	data, err := os.ReadFile(filepath.Join(output, "E04", "poc-golden-v1.0.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"fusion": {`) || !strings.Contains(string(data), `"metrics": {}`) {
		t.Fatalf("result:\n%s", data)
	}
	for _, want := range []string{
		"pipeline: multimodal",
		"test cases processed: 2",
		"outputs: audio observations=4 visual observations=4 context events=4",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunMultimodalSingleModalityConfigs(t *testing.T) {
	for name, tc := range map[string]struct {
		audio, vision bool
		want          string
	}{
		"audio only":  {true, false, "outputs: audio observations=4 visual observations=0 context events=4"},
		"vision only": {false, true, "outputs: audio observations=0 visual observations=4 context events=4"},
	} {
		t.Run(name, func(t *testing.T) {
			var f fakeRegs
			var stdout, stderr bytes.Buffer
			args := multimodalArgs(filepath.Join(t.TempDir(), "results"), writeMultimodalConfig(t, tc.audio, tc.vision, "fake"))
			if err := runWith(args, &stdout, &stderr, f.registries(t)); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(stdout.String(), tc.want) {
				t.Fatalf("stdout:\n%s", stdout.String())
			}
		})
	}
}

func TestRunMultimodalFailures(t *testing.T) {
	cases := []struct {
		name  string
		args  func(t *testing.T, output string) []string
		usage bool
		want  string
	}{
		{
			name: "context option without multimodal",
			args: func(t *testing.T, output string) []string {
				return append(baseArgs(output), "--context-option", "model=m.gguf")
			},
			usage: true, want: "--context-option is only valid with --pipeline multimodal",
		},
		{
			name: "context option with audio pipeline",
			args: func(t *testing.T, output string) []string {
				return append(withFlag(withFlag(baseArgs(output), "--pipeline", "audio"), "--config", writeAudioConfig(t, "fake")),
					"--context-option", "model=m.gguf")
			},
			usage: true, want: "--context-option is only valid with --pipeline multimodal",
		},
		{
			name: "audio option message names multimodal",
			args: func(t *testing.T, output string) []string {
				return append(baseArgs(output), "--audio-option", "model=m.bin")
			},
			usage: true, want: "--audio-option is only valid with --pipeline audio or multimodal",
		},
		{
			name: "vision option message names multimodal",
			args: func(t *testing.T, output string) []string {
				return append(baseArgs(output), "--vision-option", "model=m.gguf")
			},
			usage: true, want: "--vision-option is only valid with --pipeline vision or multimodal",
		},
		{
			name: "malformed context option",
			args: func(t *testing.T, output string) []string {
				return append(multimodalArgs(output, writeMultimodalConfig(t, true, true, "fake")), "--context-option", "model")
			},
			usage: true, want: `want key=value, got "model"`,
		},
		{
			name: "duplicate context option",
			args: func(t *testing.T, output string) []string {
				return append(multimodalArgs(output, writeMultimodalConfig(t, true, true, "fake")),
					"--context-option", "model=a", "--context-option", "model=b")
			},
			usage: true, want: `duplicate option "model"`,
		},
		{
			name: "TBD fusion provider",
			args: func(t *testing.T, output string) []string {
				return multimodalArgs(output, writeMultimodalConfig(t, true, true, "TBD"))
			},
			want: `unknown context reasoning provider "TBD"; available: fake`,
		},
		{
			name: "committed TBD configuration",
			args: func(t *testing.T, output string) []string { return multimodalArgs(output, configFile) },
			want: "vision.sampling",
		},
		{
			name: "audio option for a vision-only experiment",
			args: func(t *testing.T, output string) []string {
				return append(multimodalArgs(output, writeMultimodalConfig(t, false, true, "fake")), "--audio-option", "model=m.bin")
			},
			want: "--audio-option given but experiment E02 disables audio",
		},
		{
			name: "vision option for an audio-only experiment",
			args: func(t *testing.T, output string) []string {
				return append(multimodalArgs(output, writeMultimodalConfig(t, true, false, "fake")), "--vision-option", "model=m.gguf")
			},
			want: "--vision-option given but experiment E01 disables vision",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var f fakeRegs
			var stdout, stderr bytes.Buffer
			err := runWith(tc.args(t, filepath.Join(t.TempDir(), "results")), &stdout, &stderr, f.registries(t))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v does not contain %q", err, tc.want)
			}
			var usage usageError
			if errors.As(err, &usage) != tc.usage {
				t.Fatalf("usage error = %t, want %t", !tc.usage, tc.usage)
			}
		})
	}
}

func TestRunMultimodalDefaultRegistriesRejectMissingModel(t *testing.T) {
	config := writeMultimodalConfig(t, true, true, "llama-cpp")
	var stdout, stderr bytes.Buffer
	err := run(multimodalArgs(filepath.Join(t.TempDir(), "results"), config), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), `context reasoning provider "llama-cpp": option "model"`) {
		t.Fatalf("err: %v", err)
	}
}
