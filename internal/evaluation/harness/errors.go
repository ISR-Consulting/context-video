package harness

import (
	"errors"
	"strings"
)

// Stage identifies where a harness run failed.
type Stage string

const (
	StageSpecs          Stage = "specs"
	StageDataset        Stage = "dataset"
	StageCancelled      Stage = "cancelled"
	StagePipeline       Stage = "pipeline"
	StageOutputSchema   Stage = "output-schema"
	StageOutputDomain   Stage = "output-domain"
	StageOutputEvidence Stage = "output-evidence"
	StageOutputContent  Stage = "output-content"
	StageResult         Stage = "result"
	StagePersist        Stage = "persist"
)

// ErrResultExists reports that a result file is already present at the
// destination. The harness never overwrites results.
var ErrResultExists = errors.New("result already exists")

// Error is a stage-qualified harness failure. TestCaseID and Path are set when
// the failure belongs to one test case or output value. Err is the cause.
type Error struct {
	Stage      Stage
	TestCaseID string
	Path       string
	Err        error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	parts := []string{"harness " + string(e.Stage)}
	if e.TestCaseID != "" {
		parts = append(parts, "test case "+e.TestCaseID)
	}
	if e.Path != "" {
		parts = append(parts, e.Path)
	}
	if e.Err != nil {
		parts = append(parts, e.Err.Error())
	}
	return strings.Join(parts, ": ")
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
