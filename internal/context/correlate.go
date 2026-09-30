package context

import (
	"errors"
	"fmt"

	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// CorrelationInput is the perception output for one MediaSegment.
type CorrelationInput struct {
	Segment contracts.MediaSegment
	Audio   []contracts.AudioObservation
	Visual  []contracts.VisualObservation
}

// Correlator groups observations into reasoning units.
type Correlator interface {
	Correlate(in CorrelationInput) ([]EvidenceGroup, error)
}

// SegmentCorrelator makes one reasoning unit of one MediaSegment. Every
// observation must have the segment's content and exactly the segment's
// window; equality rather than closed-interval overlap keeps an adjacent
// window, which shares an endpoint, out of the unit. Visual frames must lie in
// the closed window. Duplicate observation IDs and duplicate frame timestamps
// within one observation are rejected. It returns no group when nothing in
// the segment is usable evidence.
type SegmentCorrelator struct{}

// Correlate returns zero or one EvidenceGroup. Observations are only read.
func (SegmentCorrelator) Correlate(in CorrelationInput) ([]EvidenceGroup, error) {
	seg := in.Segment
	fail := func(observationID string, err error) error {
		e := SegmentError(StageCorrelate, seg, "", err)
		e.ObservationID = observationID
		return e
	}
	if err := contracts.Validate(seg); err != nil {
		return nil, fail("", fmt.Errorf("invalid segment: %w", err))
	}
	if seg.SegmentID == "" {
		return nil, fail("", errors.New("segment id must not be blank"))
	}

	seen := make(map[string]struct{}, len(in.Audio)+len(in.Visual))
	admit := func(id string, content contracts.ContentRef, window contracts.TimeWindow, validate func() error) error {
		if id == "" {
			return fail("", errors.New("observationId must not be blank"))
		}
		if _, dup := seen[id]; dup {
			return fail(id, errors.New("duplicate observationId"))
		}
		seen[id] = struct{}{}
		if err := validate(); err != nil {
			return fail(id, fmt.Errorf("invalid observation: %w", err))
		}
		if content != seg.Content {
			return fail(id, fmt.Errorf("content %s/%s does not match segment content %s/%s",
				content.ContentID, content.ContentType, seg.Content.ContentID, seg.Content.ContentType))
		}
		if window != seg.Window {
			return fail(id, fmt.Errorf("window [%d, %d] is not the segment window [%d, %d]",
				window.StartMs, window.EndMs, seg.Window.StartMs, seg.Window.EndMs))
		}
		return nil
	}

	var evidence []Evidence
	for _, obs := range in.Audio {
		if err := admit(obs.ObservationID, obs.Content, obs.Window, func() error { return contracts.Validate(obs) }); err != nil {
			return nil, err
		}
		evidence = append(evidence, ProjectAudio(obs)...)
	}
	for _, obs := range in.Visual {
		if err := admit(obs.ObservationID, obs.Content, obs.Window, func() error { return contracts.Validate(obs) }); err != nil {
			return nil, err
		}
		frames := make(map[int64]struct{}, len(obs.Frames))
		for _, frame := range obs.Frames {
			if _, dup := frames[frame.TimestampMs]; dup {
				return nil, fail(obs.ObservationID, fmt.Errorf("duplicate frame timestamp %dms", frame.TimestampMs))
			}
			frames[frame.TimestampMs] = struct{}{}
			if frame.TimestampMs < seg.Window.StartMs || frame.TimestampMs > seg.Window.EndMs {
				return nil, fail(obs.ObservationID, fmt.Errorf("frame %dms is outside the segment window", frame.TimestampMs))
			}
		}
		evidence = append(evidence, ProjectVisual(obs)...)
	}
	if len(evidence) == 0 {
		return nil, nil
	}
	SortEvidence(evidence)
	return []EvidenceGroup{{
		SegmentID: seg.SegmentID,
		Content:   seg.Content,
		Window:    seg.Window,
		Evidence:  evidence,
	}}, nil
}

var _ Correlator = SegmentCorrelator{}
