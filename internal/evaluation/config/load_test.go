package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var experimentsDir = filepath.Join("..", "..", "..", "configs", "experiments")

func TestCommittedExperimentConfigsLoadUnchanged(t *testing.T) {
	cases := []struct {
		file string
		want Config
	}{
		{
			file: "audio-only-5s.yaml",
			want: Config{
				ExperimentID: "E01", ExperimentName: "audio-only-5s",
				IngestionMode: IngestionLiveSimulation, WindowSizeRaw: "5s", WindowSize: 5 * time.Second,
				Audio:          AudioConfig{Enabled: true, Provider: "whisper-cpp"},
				FusionProvider: "llama-cpp", ContextEventSelector: "v1", ContextEventSchemaVersion: "1.0",
			},
		},
		{
			file: "vision-only-5s.yaml",
			want: Config{
				ExperimentID: "E02", ExperimentName: "vision-only-5s",
				IngestionMode: IngestionLiveSimulation, WindowSizeRaw: "5s", WindowSize: 5 * time.Second,
				Vision:         VisionConfig{Enabled: true, Provider: "llama-mtmd", Sampling: "uniform:2"},
				FusionProvider: "llama-cpp", ContextEventSelector: "v1", ContextEventSchemaVersion: "1.0",
			},
		},
		{
			file: "multimodal-2s.yaml",
			want: multimodal("E03", "multimodal-2s", "2s", 2*time.Second, "uniform:1"),
		},
		{
			file: "multimodal-5s.yaml",
			want: multimodal("E04", "multimodal-5s", "5s", 5*time.Second, "uniform:2"),
		},
		{
			file: "multimodal-10s.yaml",
			want: multimodal("E05", "multimodal-10s", "10s", 10*time.Second, "uniform:4"),
		},
	}
	entries, err := os.ReadDir(experimentsDir)
	if err != nil {
		t.Fatal(err)
	}
	var yamlFiles int
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".yaml") {
			yamlFiles++
		}
	}
	if yamlFiles != len(cases) {
		t.Fatalf("committed experiment configs: %d, covered: %d", yamlFiles, len(cases))
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			got, err := LoadFile(filepath.Join(experimentsDir, tc.file))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("config:\n got %#v\nwant %#v", got, tc.want)
			}
		})
	}
}

func multimodal(id, name, raw string, size time.Duration, sampling string) Config {
	return Config{
		ExperimentID: id, ExperimentName: name,
		IngestionMode: IngestionLiveSimulation, WindowSizeRaw: raw, WindowSize: size,
		Audio:          AudioConfig{Enabled: true, Provider: "whisper-cpp"},
		Vision:         VisionConfig{Enabled: true, Provider: "llama-mtmd", Sampling: sampling},
		FusionProvider: "llama-cpp", ContextEventSelector: "v1", ContextEventSchemaVersion: "1.0",
	}
}

func TestLoadFileMissing(t *testing.T) {
	_, err := LoadFile(filepath.Join(t.TempDir(), "missing.yaml"))
	var cfgErr *Error
	if !errors.As(err, &cfgErr) || cfgErr.Stage != StageRead {
		t.Fatalf("error: %v", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cause not preserved: %v", err)
	}
}

const validYAML = `experiment:
  id: E04
  name: multimodal-5s
ingestion:
  mode: LIVE_SIMULATION
window:
  size: 5s
audio:
  enabled: true
  provider: TBD
vision:
  enabled: true
  provider: TBD
  sampling: TBD
fusion:
  provider: TBD
schema:
  context_event: v1
`

func TestDecodeFailures(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "malformed", raw: "experiment: [", want: ""},
		{name: "empty", raw: "", want: "empty document"},
		{name: "duplicate key", raw: strings.Replace(validYAML, "  name: multimodal-5s\n", "  name: multimodal-5s\n  name: again\n", 1), want: "already defined"},
		{name: "trailing document", raw: validYAML + "---\nexperiment: {}\n", want: "exactly one YAML document"},
		{name: "unknown top-level field", raw: validYAML + "extra: true\n", want: "field extra not found"},
		{name: "unknown nested field", raw: strings.Replace(validYAML, "  size: 5s\n", "  size: 5s\n  overlap: 1s\n", 1), want: "field overlap not found"},
		{name: "type mismatch", raw: strings.Replace(validYAML, "audio:\n  enabled: true", "audio:\n  enabled: maybe", 1), want: "cannot unmarshal"},
		{name: "section type mismatch", raw: strings.Replace(validYAML, "fusion:\n  provider: TBD", "fusion: TBD", 1), want: "cannot unmarshal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse("test.yaml", []byte(tc.raw))
			assertStage(t, err, StageDecode, tc.want)
		})
	}
}

func TestParseIsDeterministic(t *testing.T) {
	first, err := Parse("a.yaml", []byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Parse("b.yaml", []byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("parses differ: %#v %#v", first, second)
	}
	_, errA := Parse("bad.yaml", []byte("experiment: {}\n"))
	_, errB := Parse("bad.yaml", []byte("experiment: {}\n"))
	if errA == nil || errA.Error() != errB.Error() {
		t.Fatalf("errors differ: %v / %v", errA, errB)
	}
}

func assertStage(t *testing.T, err error, stage Stage, contains string) {
	t.Helper()
	var cfgErr *Error
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected *config.Error, got %T: %v", err, err)
	}
	if cfgErr.Stage != stage {
		t.Fatalf("stage: got %q want %q (%v)", cfgErr.Stage, stage, err)
	}
	if contains != "" && !strings.Contains(err.Error(), contains) {
		t.Fatalf("error %q does not contain %q", err.Error(), contains)
	}
}
