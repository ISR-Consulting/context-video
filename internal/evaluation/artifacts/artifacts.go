// Package artifacts persists the raw outputs of an experiment run next to
// its ExperimentResult: every validated observation and ContextEvent as JSON
// Lines, the per-segment trace, the operational summary and a run manifest.
// Nothing is ever overwritten, and no absolute host path is written.
package artifacts

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/ISR-Consulting/context-video/internal/evaluation/harness"
	"github.com/ISR-Consulting/context-video/internal/telemetry"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// ManifestFormatVersion identifies the RunManifest JSON shape.
const ManifestFormatVersion = "m08-run-manifest-1"

// ErrExists reports an experiment directory that already holds a run.
var ErrExists = errors.New("experiment output directory already exists")

// Dir is the output directory of one experiment run: <root>/<experiment-id>.
type Dir struct {
	path   string
	traces map[string]*os.File
}

// Create creates <root>/<experimentID>. It fails with ErrExists when the
// directory is already there, so a finished run is never mixed with another.
func Create(root, experimentID string) (*Dir, error) {
	if err := safeComponent(experimentID); err != nil {
		return nil, fmt.Errorf("artifacts: experiment id: %w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("artifacts: create %s: %w", root, err)
	}
	path := filepath.Join(root, experimentID)
	if err := os.Mkdir(path, 0o755); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("artifacts: %s: %w; choose another --output", path, ErrExists)
		}
		return nil, fmt.Errorf("artifacts: create %s: %w", path, err)
	}
	return &Dir{path: path, traces: map[string]*os.File{}}, nil
}

// Path returns the experiment directory.
func (d *Dir) Path() string { return d.path }

// WriteTrace appends record to trace/<test-case-id>.jsonl and syncs it, so a
// crashed or cancelled run keeps the trace of every finished segment.
func (d *Dir) WriteTrace(record telemetry.SegmentRecord) error {
	f, ok := d.traces[record.TestCaseID]
	if !ok {
		if err := safeComponent(record.TestCaseID); err != nil {
			return fmt.Errorf("artifacts: test case id: %w", err)
		}
		dir := filepath.Join(d.path, "trace")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("artifacts: %w", err)
		}
		var err error
		f, err = os.OpenFile(filepath.Join(dir, record.TestCaseID+".jsonl"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return fmt.Errorf("artifacts: %w", err)
		}
		d.traces[record.TestCaseID] = f
	}
	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("artifacts: encode trace: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("artifacts: write trace: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("artifacts: sync trace: %w", err)
	}
	return nil
}

// Close closes the open trace files.
func (d *Dir) Close() error {
	var errs []error
	for id, f := range d.traces {
		if err := f.Close(); err != nil {
			errs = append(errs, err)
		}
		delete(d.traces, id)
	}
	return errors.Join(errs...)
}

// WriteRaw writes raw/<test-case-id>/{audio-observations,visual-observations,
// context-events}.jsonl for every case, one contract object per line in
// emission order. Every line is re-validated against the JSON Schemas.
func (d *Dir) WriteRaw(validator *contracts.Validator, cases []harness.CaseOutput) error {
	if validator == nil {
		return errors.New("artifacts: nil validator")
	}
	for _, c := range cases {
		if err := safeComponent(c.TestCaseID); err != nil {
			return fmt.Errorf("artifacts: test case id: %w", err)
		}
		dir := filepath.Join(d.path, "raw", c.TestCaseID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("artifacts: %w", err)
		}
		files := []struct {
			name  string
			kind  contracts.Kind
			items []any
		}{
			{"audio-observations.jsonl", contracts.KindAudioObservation, toAny(c.Output.AudioObservations)},
			{"visual-observations.jsonl", contracts.KindVisualObservation, toAny(c.Output.VisualObservations)},
			{"context-events.jsonl", contracts.KindContextEventV1, toAny(c.Output.ContextEvents)},
		}
		for _, file := range files {
			if err := writeJSONLines(filepath.Join(dir, file.name), validator, file.kind, file.items); err != nil {
				return err
			}
		}
	}
	return nil
}

// WriteJSON writes v as indented JSON to name inside the directory.
func (d *Dir) WriteJSON(name string, v any) error {
	if err := safeComponent(name); err != nil {
		return fmt.Errorf("artifacts: file name: %w", err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("artifacts: encode %s: %w", name, err)
	}
	return writeNew(filepath.Join(d.path, name), append(data, '\n'))
}

func toAny[T any](items []T) []any {
	out := make([]any, len(items))
	for i := range items {
		out[i] = items[i]
	}
	return out
}

func writeJSONLines(path string, validator *contracts.Validator, kind contracts.Kind, items []any) error {
	var buf []byte
	for i, item := range items {
		line, err := json.Marshal(item)
		if err != nil {
			return fmt.Errorf("artifacts: %s line %d: %w", path, i+1, err)
		}
		if err := validator.ValidateJSON(kind, line); err != nil {
			return fmt.Errorf("artifacts: %s line %d: %w", path, i+1, err)
		}
		buf = append(append(buf, line...), '\n')
	}
	return writeNew(path, buf)
}

func writeNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("artifacts: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("artifacts: write %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("artifacts: sync %s: %w", path, err)
	}
	return f.Close()
}

// safeComponent accepts [A-Za-z0-9][A-Za-z0-9._-]* so an identifier can never
// introduce a separator, traversal segment or hidden file.
func safeComponent(value string) error {
	if value == "" {
		return errors.New("must not be empty")
	}
	for i, r := range value {
		alnum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if alnum || (i > 0 && (r == '.' || r == '_' || r == '-')) {
			continue
		}
		return fmt.Errorf("%q is not a safe path component", value)
	}
	return nil
}

// RunManifest identifies how one experiment run was executed. Adapter
// options that name existing files are recorded by base name and SHA-256
// only, never by host path.
type RunManifest struct {
	FormatVersion string    `json:"formatVersion"`
	ExperimentID  string    `json:"experimentId"`
	Pipeline      string    `json:"pipeline"`
	Config        string    `json:"config"`
	Dataset       Dataset   `json:"dataset"`
	Pacing        string    `json:"pacing"`
	Speed         *float64  `json:"speed,omitempty"`
	SegmentErrors string    `json:"segmentErrors"`
	CostPerHour   *float64  `json:"costPerHour,omitempty"`
	StartedAt     time.Time `json:"startedAt"`
	FinishedAt    time.Time `json:"finishedAt"`
	Status        string    `json:"status"`
	Error         string    `json:"error,omitempty"`
	Host          Host      `json:"host"`
	Options       Options   `json:"options"`
}

// Dataset pins the golden dataset a run used.
type Dataset struct {
	DatasetID      string `json:"datasetId"`
	DatasetVersion string `json:"datasetVersion"`
	ManifestPath   string `json:"manifestPath"`
}

// Host describes the machine that ran the harness.
type Host struct {
	GOOS      string `json:"goos"`
	GOARCH    string `json:"goarch"`
	NumCPU    int    `json:"numCpu"`
	GoVersion string `json:"goVersion"`
}

// CurrentHost describes this process's machine.
func CurrentHost() Host {
	return Host{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, NumCPU: runtime.NumCPU(), GoVersion: runtime.Version()}
}

// Options are the sanitized adapter options per modality.
type Options struct {
	Audio   map[string]OptionValue `json:"audio,omitempty"`
	Vision  map[string]OptionValue `json:"vision,omitempty"`
	Context map[string]OptionValue `json:"context,omitempty"`
}

// OptionValue is either a literal value or a file identified by base name
// and content hash.
type OptionValue struct {
	Value  string `json:"value,omitempty"`
	File   string `json:"file,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

// DescribeOptions sanitizes opts: a value naming an existing regular file
// (a model, projector or binary) becomes its base name and SHA-256, and a
// value that looks like a path but does not exist keeps only its base name.
func DescribeOptions(opts map[string]string) (map[string]OptionValue, error) {
	if len(opts) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(opts))
	for k := range opts {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make(map[string]OptionValue, len(opts))
	for _, k := range keys {
		v := opts[k]
		info, err := os.Stat(v)
		switch {
		case err == nil && info.Mode().IsRegular():
			sum, err := fileSHA256(v)
			if err != nil {
				return nil, fmt.Errorf("artifacts: hash option %q: %w", k, err)
			}
			out[k] = OptionValue{File: filepath.Base(v), SHA256: sum}
		case filepath.IsAbs(v) || filepath.Base(v) != v:
			out[k] = OptionValue{File: filepath.Base(v)}
		default:
			out[k] = OptionValue{Value: v}
		}
	}
	return out, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, bufio.NewReaderSize(f, 1<<20)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
