package context

import (
	"fmt"
	"strings"

	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// Stage identifies which step of multimodal context reasoning failed.
type Stage string

const (
	StageAudio      Stage = "audio"
	StageVision     Stage = "vision"
	StageCorrelate  Stage = "correlate"
	StageReason     Stage = "reason"
	StageMapContext Stage = "map-context"
)

// Error is a stage-qualified failure. The identifying fields are set when
// known; Err is the cause and is returned by Unwrap, so errors.Is(err,
// ctx.Err()) keeps detecting cancellation through it.
type Error struct {
	Stage         Stage
	ContentID     string
	SegmentID     string
	Window        *contracts.TimeWindow
	ObservationID string
	Provider      string
	Err           error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	parts := []string{"context " + string(e.Stage)}
	if e.ContentID != "" {
		parts = append(parts, "content "+e.ContentID)
	}
	if e.SegmentID != "" {
		parts = append(parts, "segment "+e.SegmentID)
	}
	if e.Window != nil {
		parts = append(parts, fmt.Sprintf("window [%d, %d]", e.Window.StartMs, e.Window.EndMs))
	}
	if e.ObservationID != "" {
		parts = append(parts, "observation "+e.ObservationID)
	}
	if e.Provider != "" {
		parts = append(parts, "provider "+e.Provider)
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

// SegmentError returns an Error for stage that identifies segment.
func SegmentError(stage Stage, segment contracts.MediaSegment, provider string, err error) *Error {
	window := segment.Window
	return &Error{
		Stage:     stage,
		ContentID: segment.Content.ContentID,
		SegmentID: segment.SegmentID,
		Window:    &window,
		Provider:  provider,
		Err:       err,
	}
}
