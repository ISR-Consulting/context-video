package harness

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/ISR-Consulting/context-video/internal/evaluation/config"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// NewResult builds a canonical ExperimentResult with no measured metrics.
func NewResult(experimentID string, startedAt time.Time, configuration map[string]any) contracts.ExperimentResult {
	return contracts.ExperimentResult{
		ExperimentID:  experimentID,
		StartedAt:     startedAt.UTC(),
		Configuration: configuration,
		Metrics:       contracts.ExperimentMetrics{},
	}
}

// ValidateResult applies M01 domain and JSON Schema validation to result.
func ValidateResult(validator *contracts.Validator, result contracts.ExperimentResult) error {
	_, err := EncodeResult(validator, result)
	return err
}

// EncodeResult validates result and returns its canonical JSON: two-space
// indentation, sorted map keys and exactly one trailing newline.
func EncodeResult(validator *contracts.Validator, result contracts.ExperimentResult) ([]byte, error) {
	if validator == nil {
		return nil, &Error{Stage: StageResult, Err: errors.New("nil validator")}
	}
	if err := contracts.Validate(result); err != nil {
		return nil, &Error{Stage: StageResult, Err: err}
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, &Error{Stage: StageResult, Err: err}
	}
	if err := validator.ValidateJSON(contracts.KindExperimentResult, data); err != nil {
		return nil, &Error{Stage: StageResult, Err: err}
	}
	return append(data, '\n'), nil
}

// ResultPath returns <outputDir>/<experiment-id>/<dataset-id>-v<dataset-version>.json.
// Identifiers must be safe single path components.
func ResultPath(outputDir, experimentID string, dataset config.DatasetIdentity) (string, error) {
	for _, component := range []struct{ name, value string }{
		{"experiment id", experimentID},
		{"dataset id", dataset.DatasetID},
		{"dataset version", dataset.DatasetVersion},
	} {
		if err := safeComponent(component.value); err != nil {
			return "", &Error{Stage: StagePersist, Path: component.name, Err: err}
		}
	}
	name := dataset.DatasetID + "-v" + dataset.DatasetVersion + ".json"
	return filepath.Join(outputDir, experimentID, name), nil
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

// Persister writes canonical ExperimentResult JSON under a root directory.
type Persister struct {
	root      string
	validator *contracts.Validator
	// link publishes the synced temporary file without overwriting an
	// existing destination.
	link func(oldname, newname string) error
}

// NewPersister returns a Persister rooted at outputDir.
func NewPersister(validator *contracts.Validator, outputDir string) (*Persister, error) {
	if validator == nil {
		return nil, errors.New("harness: nil validator")
	}
	if outputDir == "" {
		return nil, errors.New("harness: empty output directory")
	}
	return &Persister{root: outputDir, validator: validator, link: os.Link}, nil
}

// Write validates and atomically persists result, returning the file path.
// An existing destination is never overwritten; ErrResultExists is returned
// instead. Temporary files are removed on every failure.
func (p *Persister) Write(result contracts.ExperimentResult, dataset config.DatasetIdentity) (string, error) {
	data, err := EncodeResult(p.validator, result)
	if err != nil {
		return "", err
	}
	path, err := ResultPath(p.root, result.ExperimentID, dataset)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(path); err == nil {
		return "", &Error{Stage: StagePersist, Path: path, Err: ErrResultExists}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", &Error{Stage: StagePersist, Path: path, Err: err}
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", &Error{Stage: StagePersist, Path: dir, Err: fmt.Errorf("create directory: %w", err)}
	}
	if err := p.publish(dir, path, data); err != nil {
		return "", err
	}
	return path, nil
}

func (p *Persister) publish(dir, path string, data []byte) (err error) {
	tmp, err := os.CreateTemp(dir, ".result-*.tmp")
	if err != nil {
		return &Error{Stage: StagePersist, Path: dir, Err: fmt.Errorf("create temporary file: %w", err)}
	}
	tmpName := tmp.Name()
	defer func() {
		if removeErr := os.Remove(tmpName); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) && err == nil {
			err = &Error{Stage: StagePersist, Path: tmpName, Err: fmt.Errorf("remove temporary file: %w", removeErr)}
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return &Error{Stage: StagePersist, Path: tmpName, Err: fmt.Errorf("write temporary file: %w", err)}
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return &Error{Stage: StagePersist, Path: tmpName, Err: fmt.Errorf("sync temporary file: %w", err)}
	}
	if err := tmp.Close(); err != nil {
		return &Error{Stage: StagePersist, Path: tmpName, Err: fmt.Errorf("close temporary file: %w", err)}
	}
	if err := p.link(tmpName, path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return &Error{Stage: StagePersist, Path: path, Err: ErrResultExists}
		}
		return &Error{Stage: StagePersist, Path: path, Err: fmt.Errorf("publish result: %w", err)}
	}
	return nil
}
