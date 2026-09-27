package audio

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// ObservationID returns the deterministic audio observation identifier for a
// segment: "aud:" followed by the segment ID.
func ObservationID(segment contracts.MediaSegment) string {
	return "aud:" + segment.SegmentID
}

// NewObservation maps a transcription of segment onto an AudioObservation.
// Content and window come from the segment. Language, confidence and model are
// set only when the transcription carries them; nothing is defaulted or
// derived. The result passes contracts.Validate.
func NewObservation(segment contracts.MediaSegment, t Transcription, pipelineVersion string) (contracts.AudioObservation, error) {
	var errs []error
	if strings.TrimSpace(segment.SegmentID) == "" {
		errs = append(errs, errors.New("segment id must not be blank"))
	}
	if strings.TrimSpace(t.Provider) == "" {
		errs = append(errs, errors.New("provider must not be blank"))
	}
	if strings.TrimSpace(pipelineVersion) == "" {
		errs = append(errs, errors.New("pipeline version must not be blank"))
	}
	if c := t.Confidence; c != nil && (math.IsNaN(*c) || *c < 0 || *c > 1) {
		errs = append(errs, fmt.Errorf("confidence %v must be within [0, 1]", *c))
	}
	if len(errs) > 0 {
		return contracts.AudioObservation{}, fmt.Errorf("audio observation for segment %q: %w", segment.SegmentID, errors.Join(errs...))
	}

	transcript := contracts.Transcript{Text: t.Text}
	if t.Language != "" {
		language := t.Language
		transcript.Language = &language
	}
	if t.Confidence != nil {
		confidence := *t.Confidence
		transcript.Confidence = &confidence
	}
	provenance := contracts.ObservationProvenance{Provider: t.Provider, PipelineVersion: pipelineVersion}
	if t.Model != "" {
		model := t.Model
		provenance.Model = &model
	}
	observation := contracts.AudioObservation{
		ObservationID: ObservationID(segment),
		Content:       segment.Content,
		Window:        segment.Window,
		Transcript:    transcript,
		Provenance:    provenance,
	}
	if err := contracts.Validate(observation); err != nil {
		return contracts.AudioObservation{}, fmt.Errorf("audio observation for segment %q: %w", segment.SegmentID, err)
	}
	return observation, nil
}
