package context

import (
	"cmp"
	"slices"
	"strings"

	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// EvidenceKind is the modality of the observation an evidence item projects.
type EvidenceKind string

const (
	EvidenceAudio  EvidenceKind = "AUDIO"
	EvidenceVisual EvidenceKind = "VISUAL"
)

// Facet is the part of an observation an evidence item projects.
type Facet string

const (
	// FacetTranscript is the transcript text of an AudioObservation.
	FacetTranscript Facet = "TRANSCRIPT"
	// FacetFrameDescription is the description of a visual frame. It is
	// context for reasoning, never a detection.
	FacetFrameDescription Facet = "FRAME_DESCRIPTION"
	// FacetDetection is one structured observation of a visual frame.
	FacetDetection Facet = "DETECTION"
)

// EvidenceRef identifies the evidence a candidate cites: an audio observation
// (TimestampMs nil) or one frame of a visual observation.
type EvidenceRef struct {
	ObservationID string
	TimestampMs   *int64
}

// Evidence is the internal, provider-neutral projection of one part of an
// observation. It is never serialized as a contract. Values are copied from
// the observation verbatim; Confidence is descriptive metadata only and is not
// a calibrated probability.
type Evidence struct {
	Kind          EvidenceKind
	Facet         Facet
	ObservationID string
	// TimestampMs is the frame time for visual evidence and nil for audio.
	TimestampMs *int64
	// Window is the window of the source observation.
	Window contracts.TimeWindow
	// Type and Value are set for detections only.
	Type  string
	Value string
	// Text is the transcript or frame description.
	Text string
	// Language is the transcript language when the provider reported one.
	Language string
	// Confidence is the provider value, nil when the provider supplied none.
	Confidence *float64
	// Item orders evidence within one observation and timestamp.
	Item int
}

// Ref returns the reference that cites the observation or frame of e.
func (e Evidence) Ref() EvidenceRef {
	return EvidenceRef{ObservationID: e.ObservationID, TimestampMs: clonePtr(e.TimestampMs)}
}

// EvidenceGroup is the evidence of one reasoning unit: the observations of one
// MediaSegment, in canonical order.
type EvidenceGroup struct {
	SegmentID string
	Content   contracts.ContentRef
	Window    contracts.TimeWindow
	Evidence  []Evidence
}

// Clone returns a deep copy of g so a reasoner cannot alter the group the
// engine grounds candidates against.
func (g EvidenceGroup) Clone() EvidenceGroup {
	out := g
	out.Evidence = make([]Evidence, len(g.Evidence))
	for i, e := range g.Evidence {
		e.TimestampMs = clonePtr(e.TimestampMs)
		e.Confidence = clonePtr(e.Confidence)
		out.Evidence[i] = e
	}
	return out
}

// ProjectAudio projects an AudioObservation onto evidence. A transcript that
// is blank after trimming carries no usable semantic content and yields no
// evidence; the text is otherwise kept verbatim, provider markers included.
func ProjectAudio(obs contracts.AudioObservation) []Evidence {
	if strings.TrimSpace(obs.Transcript.Text) == "" {
		return nil
	}
	e := Evidence{
		Kind:          EvidenceAudio,
		Facet:         FacetTranscript,
		ObservationID: obs.ObservationID,
		Window:        obs.Window,
		Text:          obs.Transcript.Text,
		Confidence:    clonePtr(obs.Transcript.Confidence),
	}
	if obs.Transcript.Language != nil {
		e.Language = *obs.Transcript.Language
	}
	return []Evidence{e}
}

// ProjectVisual projects every frame of a VisualObservation onto evidence: the
// frame description first when present, then each detection in its original
// order. A frame with neither yields no evidence.
func ProjectVisual(obs contracts.VisualObservation) []Evidence {
	var out []Evidence
	for _, frame := range obs.Frames {
		item := 0
		base := Evidence{Kind: EvidenceVisual, ObservationID: obs.ObservationID, Window: obs.Window}
		if frame.Description != nil && strings.TrimSpace(*frame.Description) != "" {
			e := base
			e.Facet = FacetFrameDescription
			e.TimestampMs = ptr(frame.TimestampMs)
			e.Text = *frame.Description
			e.Item = item
			out = append(out, e)
		}
		item++
		for _, d := range frame.Observations {
			e := base
			e.Facet = FacetDetection
			e.TimestampMs = ptr(frame.TimestampMs)
			e.Type = string(d.Type)
			e.Value = d.Value
			e.Confidence = ptr(d.Confidence)
			e.Item = item
			out = append(out, e)
			item++
		}
	}
	return out
}

// SortEvidence orders evidence canonically: effective timestamp (window start
// for audio, frame time for visual), then kind (AUDIO before VISUAL), then
// observationId, then item.
func SortEvidence(evidence []Evidence) {
	slices.SortStableFunc(evidence, func(a, b Evidence) int {
		return cmp.Or(
			cmp.Compare(effectiveTimestamp(a), effectiveTimestamp(b)),
			cmp.Compare(kindRank(a.Kind), kindRank(b.Kind)),
			strings.Compare(a.ObservationID, b.ObservationID),
			cmp.Compare(a.Item, b.Item),
		)
	})
}

func effectiveTimestamp(e Evidence) int64 {
	if e.TimestampMs != nil {
		return *e.TimestampMs
	}
	return e.Window.StartMs
}

func kindRank(k EvidenceKind) int {
	if k == EvidenceAudio {
		return 0
	}
	return 1
}

func ptr[T any](v T) *T { return &v }

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
