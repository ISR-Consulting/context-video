package artifacts

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ISR-Consulting/context-video/internal/evaluation/harness"
	"github.com/ISR-Consulting/context-video/internal/telemetry"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

const specsRoot = "../../../specs"

func validator(t *testing.T) *contracts.Validator {
	t.Helper()
	v, err := contracts.NewValidator(os.DirFS(specsRoot))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func example[T any](t *testing.T, name string) T {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(specsRoot, "examples", name))
	if err != nil {
		t.Fatal(err)
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []string
	s := bufio.NewScanner(f)
	s.Buffer(nil, 1<<20)
	for s.Scan() {
		lines = append(lines, s.Text())
	}
	return lines
}

func TestCreateRefusesExistingExperimentDir(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runs")
	d, err := Create(root, "E04")
	if err != nil {
		t.Fatal(err)
	}
	if d.Path() != filepath.Join(root, "E04") {
		t.Fatalf("path %s", d.Path())
	}
	if _, err := Create(root, "E04"); !errors.Is(err, ErrExists) {
		t.Fatalf("second create: %v", err)
	}
	if _, err := Create(root, "../E04"); err == nil {
		t.Fatal("unsafe experiment id accepted")
	}
}

func TestWriteRawValidatesEveryLine(t *testing.T) {
	v := validator(t)
	audio := example[contracts.AudioObservation](t, "audio-observation.example.json")
	visual := example[contracts.VisualObservation](t, "visual-observation.example.json")
	event := example[contracts.ContextEventV1](t, "context-event-v1.example.json")
	d, err := Create(t.TempDir(), "E04")
	if err != nil {
		t.Fatal(err)
	}
	cases := []harness.CaseOutput{{TestCaseID: "football-live-01", Output: harness.PipelineOutput{
		AudioObservations:  []contracts.AudioObservation{audio, audio},
		VisualObservations: []contracts.VisualObservation{visual},
		ContextEvents:      []contracts.ContextEventV1{event},
	}}}
	if err := d.WriteRaw(v, cases); err != nil {
		t.Fatal(err)
	}
	raw := filepath.Join(d.Path(), "raw", "football-live-01")
	for name, want := range map[string]int{"audio-observations.jsonl": 2, "visual-observations.jsonl": 1, "context-events.jsonl": 1} {
		lines := readLines(t, filepath.Join(raw, name))
		if len(lines) != want {
			t.Fatalf("%s: %d lines, want %d", name, len(lines), want)
		}
	}
	lines := readLines(t, filepath.Join(raw, "context-events.jsonl"))
	if err := v.ValidateJSON(contracts.KindContextEventV1, []byte(lines[0])); err != nil {
		t.Fatal(err)
	}
	if err := d.WriteRaw(v, cases); err == nil {
		t.Fatal("raw files overwritten")
	}

	bad := event
	bad.SchemaVersion = "9.9"
	d2, err := Create(t.TempDir(), "E04")
	if err != nil {
		t.Fatal(err)
	}
	if err := d2.WriteRaw(v, []harness.CaseOutput{{TestCaseID: "c", Output: harness.PipelineOutput{ContextEvents: []contracts.ContextEventV1{bad}}}}); err == nil ||
		!strings.Contains(err.Error(), "context-events.jsonl line 1") {
		t.Fatalf("invalid event written: %v", err)
	}
}

func TestWriteTraceAppendsPerCase(t *testing.T) {
	d, err := Create(t.TempDir(), "E01")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []telemetry.SegmentRecord{
		{TestCaseID: "a", SegmentID: "a:0-5000", Status: telemetry.StatusOK},
		{TestCaseID: "a", SegmentID: "a:5000-10000", Status: telemetry.StatusFailed},
		{TestCaseID: "b", SegmentID: "b:0-5000", Status: telemetry.StatusOK},
	} {
		if err := d.WriteTrace(r); err != nil {
			t.Fatal(err)
		}
	}
	// Records are on disk before Close.
	lines := readLines(t, filepath.Join(d.Path(), "trace", "a.jsonl"))
	if len(lines) != 2 || !strings.Contains(lines[1], `"segmentId":"a:5000-10000"`) {
		t.Fatalf("trace a: %q", lines)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if lines := readLines(t, filepath.Join(d.Path(), "trace", "b.jsonl")); len(lines) != 1 {
		t.Fatalf("trace b: %q", lines)
	}
	if err := d.WriteTrace(telemetry.SegmentRecord{TestCaseID: "../x"}); err == nil {
		t.Fatal("unsafe test case id accepted")
	}
}

func TestWriteJSONNeverOverwrites(t *testing.T) {
	d, err := Create(t.TempDir(), "E02")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.WriteJSON("summary.json", map[string]int{"segments": 3}); err != nil {
		t.Fatal(err)
	}
	if err := d.WriteJSON("summary.json", map[string]int{"segments": 4}); err == nil {
		t.Fatal("summary overwritten")
	}
	if err := d.WriteJSON("../escape.json", 1); err == nil {
		t.Fatal("unsafe name accepted")
	}
}

func TestDescribeOptionsHashesFilesAndDropsPaths(t *testing.T) {
	dir := t.TempDir()
	model := filepath.Join(dir, "model.gguf")
	if err := os.WriteFile(model, []byte("weights"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("weights"))
	got, err := DescribeOptions(map[string]string{
		"model": model, "binary": "/opt/missing/llama-completion", "language": "pt", "ctx-size": "8192",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]OptionValue{
		"model":    {File: "model.gguf", SHA256: hex.EncodeToString(sum[:])},
		"binary":   {File: "llama-completion"},
		"language": {Value: "pt"},
		"ctx-size": {Value: "8192"},
	}
	for k, w := range want {
		if got[k] != w {
			t.Fatalf("%s: %+v, want %+v", k, got[k], w)
		}
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), dir) || strings.Contains(string(data), "/opt/") {
		t.Fatalf("host path leaked: %s", data)
	}
	if none, err := DescribeOptions(nil); err != nil || none != nil {
		t.Fatalf("empty options: %v %v", none, err)
	}
}
