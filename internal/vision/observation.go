package vision

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// ObservationID returns the deterministic visual observation identifier for a
// segment: "vis:" followed by the segment ID.
func ObservationID(segment contracts.MediaSegment) string {
	return "vis:" + segment.SegmentID
}

// NewObservation maps the analysis of the frames at frameTimesMs of segment
// onto a VisualObservation. The analysis must hold exactly one frame per
// requested timestamp, in order. Every detection needs a contract type, a
// non-blank value and a provider-supplied confidence in [0, 1]; nothing is
// defaulted, clamped or repaired. The result passes contracts.Validate.
func NewObservation(segment contracts.MediaSegment, frameTimesMs []int64, a Analysis, pipelineVersion string) (contracts.VisualObservation, error) {
	fail := func(err error) (contracts.VisualObservation, error) {
		return contracts.VisualObservation{}, fmt.Errorf("visual observation for segment %q: %w", segment.SegmentID, err)
	}
	var errs []error
	if strings.TrimSpace(segment.SegmentID) == "" {
		errs = append(errs, errors.New("segment id must not be blank"))
	}
	if strings.TrimSpace(a.Provider) == "" {
		errs = append(errs, errors.New("provider must not be blank"))
	}
	if strings.TrimSpace(pipelineVersion) == "" {
		errs = append(errs, errors.New("pipeline version must not be blank"))
	}
	if len(frameTimesMs) == 0 {
		errs = append(errs, errors.New("at least one sampled frame is required"))
	}
	if len(a.Frames) != len(frameTimesMs) {
		errs = append(errs, fmt.Errorf("analysis has %d frames for %d sampled timestamps", len(a.Frames), len(frameTimesMs)))
	}
	if len(errs) > 0 {
		return fail(errors.Join(errs...))
	}

	types := DetectionTypes()
	frames := make([]contracts.VisualFrame, 0, len(a.Frames))
	for i, frame := range a.Frames {
		if frame.TimestampMs != frameTimesMs[i] {
			errs = append(errs, fmt.Errorf("frame %d timestamp %d does not match sampled timestamp %d", i, frame.TimestampMs, frameTimesMs[i]))
			continue
		}
		detections := make([]contracts.VisualDetection, 0, len(frame.Detections))
		for j, d := range frame.Detections {
			where := fmt.Sprintf("frame %dms detection %d", frame.TimestampMs, j)
			value := strings.TrimSpace(d.Value)
			switch {
			case !slices.Contains(types, d.Type):
				errs = append(errs, fmt.Errorf("%s: type %q is not a contract detection type", where, d.Type))
			case value == "":
				errs = append(errs, fmt.Errorf("%s: value must not be blank", where))
			case d.Confidence == nil:
				errs = append(errs, fmt.Errorf("%s: confidence is required and the provider supplied none", where))
			case math.IsNaN(*d.Confidence) || *d.Confidence < 0 || *d.Confidence > 1:
				errs = append(errs, fmt.Errorf("%s: confidence %v must be within [0, 1]", where, *d.Confidence))
			default:
				detections = append(detections, contracts.VisualDetection{Type: d.Type, Value: value, Confidence: *d.Confidence})
			}
		}
		out := contracts.VisualFrame{TimestampMs: frame.TimestampMs, Observations: detections}
		if description := strings.TrimSpace(frame.Description); description != "" {
			out.Description = &description
		}
		frames = append(frames, out)
	}
	if len(errs) > 0 {
		return fail(errors.Join(errs...))
	}

	provenance := contracts.ObservationProvenance{Provider: a.Provider, PipelineVersion: pipelineVersion}
	if a.Model != "" {
		model := a.Model
		provenance.Model = &model
	}
	observation := contracts.VisualObservation{
		ObservationID: ObservationID(segment),
		Content:       segment.Content,
		Window:        segment.Window,
		Frames:        frames,
		Provenance:    provenance,
	}
	if err := contracts.Validate(observation); err != nil {
		return fail(err)
	}
	return observation, nil
}
