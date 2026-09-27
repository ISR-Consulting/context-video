package media

import (
	"errors"
	"strings"
)

// Stage identifies where segmentation or replay failed.
type Stage string

const (
	StageSource    Stage = "source"
	StageWindow    Stage = "window"
	StagePacing    Stage = "pacing"
	StageSegment   Stage = "segment"
	StageCancelled Stage = "cancelled"
	StageEmit      Stage = "emit"
)

var (
	// ErrInvalidSource reports a Source that cannot be segmented.
	ErrInvalidSource = errors.New("invalid source")
	// ErrInvalidWindow reports a window size that cannot be used.
	ErrInvalidWindow = errors.New("invalid window")
	// ErrInvalidPacing reports a Pacing that cannot schedule a replay.
	ErrInvalidPacing = errors.New("invalid pacing")
)

// Error is a stage-qualified media failure. SegmentID is set when the failure
// belongs to one segment. Err is the cause.
type Error struct {
	Stage     Stage
	SegmentID string
	Err       error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	parts := []string{"media " + string(e.Stage)}
	if e.SegmentID != "" {
		parts = append(parts, "segment "+e.SegmentID)
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
