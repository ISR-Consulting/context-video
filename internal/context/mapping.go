package context

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// EventID returns the deterministic ContextEvent identifier of the ordinal-th
// (1-based) candidate of a segment: "ctx:<segmentId>:<ordinal>".
func EventID(segmentID string, ordinal int) string {
	return "ctx:" + segmentID + ":" + strconv.Itoa(ordinal)
}

// NewContextEvent maps a grounded candidate onto ContextEvent v1. Content and
// window come from the group. Evidence follows the group's canonical order,
// whatever order the reasoner cited it in: audio items carry the observation
// window and the transcript verbatim; visual items carry the frame timestamp
// and the frame description verbatim or, when the frame has none, a
// deterministic "TYPE: value; …" rendering of that frame's own detections.
// Confidences are copied from the candidate, never computed. Provenance
// records the reasoner. The result passes contracts.Validate.
func NewContextEvent(group EvidenceGroup, c Candidate, ordinal int, pipelineVersion string) (contracts.ContextEventV1, error) {
	fail := func(err error) (contracts.ContextEventV1, error) {
		return contracts.ContextEventV1{}, fmt.Errorf("context event %d of segment %q: %w", ordinal, group.SegmentID, err)
	}
	var errs []error
	if strings.TrimSpace(group.SegmentID) == "" {
		errs = append(errs, errors.New("segment id must not be blank"))
	}
	if ordinal < 1 {
		errs = append(errs, fmt.Errorf("ordinal %d must be >= 1", ordinal))
	}
	if strings.TrimSpace(pipelineVersion) == "" {
		errs = append(errs, errors.New("pipeline version must not be blank"))
	}
	if err := Ground(group, c); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fail(errors.Join(errs...))
	}

	body := contracts.ContextBody{
		Entities: make([]contracts.Entity, 0, len(c.Entities)),
		Topics:   semanticValues(c.Topics),
		Objects:  semanticValues(c.Objects),
		Brands:   semanticValues(c.Brands),
	}
	for _, e := range c.Entities {
		body.Entities = append(body.Entities, contracts.Entity{Type: e.Type, Value: e.Value, Confidence: *e.Confidence})
	}

	event := contracts.ContextEventV1{
		EventID:       EventID(group.SegmentID, ordinal),
		SchemaVersion: contracts.ContextEventSchemaVersion,
		Content:       group.Content,
		Window:        group.Window,
		Context:       body,
		Confidence:    *c.Confidence,
		Evidence:      evidenceFor(group, c.Evidence),
		Provenance:    contracts.ContextProvenance{PipelineVersion: pipelineVersion},
	}
	if p := c.Reasoning.Provider; p != "" {
		event.Provenance.FusionProvider = &p
	}
	if m := c.Reasoning.Model; m != "" {
		event.Provenance.FusionModel = &m
	}
	if v := c.Reasoning.PromptVersion; v != "" {
		event.Provenance.PromptVersion = &v
	}
	if err := contracts.Validate(event); err != nil {
		return fail(err)
	}
	return event, nil
}

func semanticValues(values []Value) []contracts.SemanticValue {
	out := make([]contracts.SemanticValue, 0, len(values))
	for _, v := range values {
		out = append(out, contracts.SemanticValue{Value: v.Value, Confidence: *v.Confidence})
	}
	return out
}

// evidenceFor returns the cited evidence in group order. refs must already be
// grounded against group.
func evidenceFor(group EvidenceGroup, refs []EvidenceRef) contracts.Evidence {
	audio := make(map[string]bool)
	visual := make(map[string]map[int64]bool)
	for _, ref := range refs {
		if ref.TimestampMs == nil {
			audio[ref.ObservationID] = true
			continue
		}
		if visual[ref.ObservationID] == nil {
			visual[ref.ObservationID] = make(map[int64]bool)
		}
		visual[ref.ObservationID][*ref.TimestampMs] = true
	}

	out := contracts.Evidence{Audio: []contracts.AudioEvidence{}, Visual: []contracts.VisualEvidence{}}
	evidence := group.Evidence
	for i := 0; i < len(evidence); {
		e := evidence[i]
		if e.Kind == EvidenceAudio {
			if audio[e.ObservationID] {
				out.Audio = append(out.Audio, contracts.AudioEvidence{
					ObservationID: e.ObservationID,
					StartMs:       e.Window.StartMs,
					EndMs:         e.Window.EndMs,
					Text:          e.Text,
				})
				delete(audio, e.ObservationID)
			}
			i++
			continue
		}
		if e.TimestampMs == nil {
			i++
			continue
		}
		j := i
		for j < len(evidence) && evidence[j].Kind == EvidenceVisual && evidence[j].ObservationID == e.ObservationID &&
			evidence[j].TimestampMs != nil && *evidence[j].TimestampMs == *e.TimestampMs {
			j++
		}
		if ts := *e.TimestampMs; visual[e.ObservationID][ts] {
			out.Visual = append(out.Visual, contracts.VisualEvidence{
				ObservationID: e.ObservationID,
				TimestampMs:   ts,
				Description:   frameDescription(evidence[i:j]),
			})
			delete(visual[e.ObservationID], ts)
		}
		i = j
	}
	return out
}

// frameDescription returns the frame description when the frame has one and
// otherwise renders the frame's detections, e.g. "OBJECT: ball; ACTION: kick".
func frameDescription(frame []Evidence) string {
	var detections []string
	for _, e := range frame {
		if e.Facet == FacetFrameDescription {
			return e.Text
		}
		if e.Facet == FacetDetection {
			detections = append(detections, e.Type+": "+e.Value)
		}
	}
	return strings.Join(detections, "; ")
}
