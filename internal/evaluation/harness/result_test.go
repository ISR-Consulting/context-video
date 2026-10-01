package harness

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ISR-Consulting/context-video/internal/evaluation/config"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

var fixtureIdentity = config.DatasetIdentity{DatasetID: "poc-golden", DatasetVersion: "1.0", ManifestPath: fixtureManifest}

func sampleResult(t *testing.T) contracts.ExperimentResult {
	t.Helper()
	return NewResult("E04", fixedStart, loadConfig(t).Snapshot(fixtureIdentity))
}

func TestEncodeResultIsCanonicalAndSchemaValid(t *testing.T) {
	validator := newValidator(t)
	data, err := EncodeResult(validator, sampleResult(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := validator.ValidateJSON(contracts.KindExperimentResult, data); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if !strings.HasSuffix(string(data), "}\n") || strings.HasSuffix(string(data), "\n\n") {
		t.Fatalf("expected exactly one trailing newline: %q", data[len(data)-3:])
	}
	want := `{
  "experimentId": "E04",
  "startedAt": "2026-09-27T12:00:00Z",
  "configuration": {
    "audio": {
      "enabled": true,
      "provider": "whisper-cpp"
    },
    "dataset": {
      "datasetId": "poc-golden",
      "datasetVersion": "1.0",
      "manifestPath": "manifests/poc-golden-v1.0.json"
    },
    "experiment": {
      "id": "E04",
      "name": "multimodal-5s"
    },
    "fusion": {
      "provider": "llama-cpp"
    },
    "ingestion": {
      "mode": "LIVE_SIMULATION"
    },
    "schema": {
      "context_event": "v1"
    },
    "vision": {
      "enabled": true,
      "provider": "llama-mtmd",
      "sampling": "uniform:2"
    },
    "window": {
      "size": "5s"
    }
  },
  "metrics": {}
}
`
	if string(data) != want {
		t.Fatalf("canonical JSON:\n%s\nwant:\n%s", data, want)
	}
	again, err := EncodeResult(validator, sampleResult(t))
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(data) {
		t.Fatal("encoding is not deterministic")
	}
}

func TestEncodeResultPreservesMeasuredZero(t *testing.T) {
	result := sampleResult(t)
	zero := 0.0
	result.Metrics.SchemaCompliance = &zero
	data, err := EncodeResult(newValidator(t), result)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Metrics map[string]any `json:"metrics"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	metrics := decoded.Metrics
	if len(metrics) != 1 || metrics["schemaCompliance"] != 0.0 {
		t.Fatalf("metrics: %#v", metrics)
	}
}

func TestEncodeResultRejectsInvalidResults(t *testing.T) {
	validator := newValidator(t)
	cases := []struct {
		name   string
		mutate func(*contracts.ExperimentResult)
	}{
		{name: "schema: empty experiment id", mutate: func(r *contracts.ExperimentResult) { r.ExperimentID = "" }},
		{name: "schema: nil configuration", mutate: func(r *contracts.ExperimentResult) { r.Configuration = nil }},
		{name: "schema: metric out of range", mutate: func(r *contracts.ExperimentResult) {
			v := 1.5
			r.Metrics.EvidenceTraceability = &v
		}},
		{name: "domain: commercial key", mutate: func(r *contracts.ExperimentResult) { r.Configuration["price"] = 10 }},
		{name: "encoding: unsupported value", mutate: func(r *contracts.ExperimentResult) { r.Configuration["bad"] = make(chan int) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := sampleResult(t)
			tc.mutate(&result)
			_, err := EncodeResult(validator, result)
			assertHarnessError(t, err, StageResult, "")
		})
	}
}

func TestResultPath(t *testing.T) {
	got, err := ResultPath("results", "E04", fixtureIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("results", "E04", "poc-golden-v1.0.json"); got != want {
		t.Fatalf("path: %q want %q", got, want)
	}
	unsafe := []struct {
		experimentID string
		identity     config.DatasetIdentity
	}{
		{"", fixtureIdentity},
		{"..", fixtureIdentity},
		{"E04/../../etc", fixtureIdentity},
		{`E04\x`, fixtureIdentity},
		{".hidden", fixtureIdentity},
		{"E04", config.DatasetIdentity{DatasetID: "a/b", DatasetVersion: "1.0"}},
		{"E04", config.DatasetIdentity{DatasetID: "poc-golden", DatasetVersion: "../1"}},
		{"E04", config.DatasetIdentity{DatasetID: "poc golden", DatasetVersion: "1.0"}},
	}
	for _, tc := range unsafe {
		if _, err := ResultPath("results", tc.experimentID, tc.identity); err == nil {
			t.Errorf("accepted unsafe components %q %#v", tc.experimentID, tc.identity)
		} else {
			assertHarnessError(t, err, StagePersist, "")
		}
	}
}

func newPersister(t *testing.T, dir string) *Persister {
	t.Helper()
	p, err := NewPersister(newValidator(t), dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func assertNoTemporaryFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".result-") {
			t.Fatalf("temporary file left behind: %s", entry.Name())
		}
	}
}

func TestPersisterWritesCanonicalResult(t *testing.T) {
	root := filepath.Join(t.TempDir(), "results")
	validator := newValidator(t)
	result := sampleResult(t)
	path, err := newPersister(t, root).Write(result, fixtureIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "E04", "poc-golden-v1.0.json"); path != want {
		t.Fatalf("path: %q want %q", path, want)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := EncodeResult(validator, result)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != string(want) {
		t.Fatalf("written bytes differ from canonical encoding")
	}
	var decoded contracts.ExperimentResult
	if err := json.Unmarshal(written, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Metrics != (contracts.ExperimentMetrics{}) {
		t.Fatalf("absent metrics not preserved: %#v", decoded.Metrics)
	}
	assertNoTemporaryFiles(t, filepath.Dir(path))
}

func TestPersisterRefusesOverwrite(t *testing.T) {
	root := t.TempDir()
	p := newPersister(t, root)
	path, err := p.Write(sampleResult(t), fixtureIdentity)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	other := sampleResult(t)
	other.Configuration["note"] = "different"
	_, err = p.Write(other, fixtureIdentity)
	assertHarnessError(t, err, StagePersist, "")
	if !errors.Is(err, ErrResultExists) {
		t.Fatalf("expected collision, got %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("existing result was modified")
	}
	assertNoTemporaryFiles(t, filepath.Dir(path))
}

func TestPersisterCollisionDuringPublish(t *testing.T) {
	root := t.TempDir()
	p := newPersister(t, root)
	p.link = func(oldname, newname string) error {
		if err := os.WriteFile(newname, []byte("winner\n"), 0o644); err != nil {
			return err
		}
		return os.Link(oldname, newname)
	}
	_, err := p.Write(sampleResult(t), fixtureIdentity)
	if !errors.Is(err, ErrResultExists) {
		t.Fatalf("expected collision, got %v", err)
	}
	winner, err := os.ReadFile(filepath.Join(root, "E04", "poc-golden-v1.0.json"))
	if err != nil || string(winner) != "winner\n" {
		t.Fatalf("concurrent winner overwritten: %q %v", winner, err)
	}
	assertNoTemporaryFiles(t, filepath.Join(root, "E04"))
}

func TestPersisterPublishFailureLeavesNoTemporaryFile(t *testing.T) {
	root := t.TempDir()
	p := newPersister(t, root)
	cause := errors.New("link not supported")
	p.link = func(string, string) error { return cause }
	_, err := p.Write(sampleResult(t), fixtureIdentity)
	assertHarnessError(t, err, StagePersist, "")
	if !errors.Is(err, cause) {
		t.Fatalf("cause not wrapped: %v", err)
	}
	dir := filepath.Join(root, "E04")
	assertNoTemporaryFiles(t, dir)
	if _, err := os.Stat(filepath.Join(dir, "poc-golden-v1.0.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("result published after failure: %v", err)
	}
}

func TestPersisterInvalidDestinations(t *testing.T) {
	t.Run("output root is a file", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "results")
		if err := os.WriteFile(file, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := newPersister(t, file).Write(sampleResult(t), fixtureIdentity)
		assertHarnessError(t, err, StagePersist, "")
	})
	t.Run("unwritable directory", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("permission checks do not apply to root")
		}
		root := t.TempDir()
		dir := filepath.Join(root, "E04")
		if err := os.Mkdir(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		_, err := newPersister(t, root).Write(sampleResult(t), fixtureIdentity)
		assertHarnessError(t, err, StagePersist, "")
		if !errors.Is(err, os.ErrPermission) {
			t.Fatalf("expected permission error, got %v", err)
		}
		assertNoTemporaryFiles(t, dir)
	})
	t.Run("invalid result is rejected before touching disk", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "results")
		result := sampleResult(t)
		result.ExperimentID = ""
		_, err := newPersister(t, root).Write(result, fixtureIdentity)
		assertHarnessError(t, err, StageResult, "")
		if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("output directory created for invalid result: %v", err)
		}
	})
	t.Run("constructor", func(t *testing.T) {
		if _, err := NewPersister(nil, "results"); err == nil {
			t.Fatal("nil validator accepted")
		}
		if _, err := NewPersister(newValidator(t), ""); err == nil {
			t.Fatal("empty output directory accepted")
		}
	})
}
