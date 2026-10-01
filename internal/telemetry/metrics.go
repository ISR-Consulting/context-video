package telemetry

import (
	"math"
	"slices"
	"sort"

	"github.com/ISR-Consulting/context-video/internal/evaluation/harness"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// SummaryFormatVersion identifies the Summary JSON shape.
const SummaryFormatVersion = "m08-summary-1"

// RunInput is everything a finished multimodal run produced.
type RunInput struct {
	ExperimentID string
	Live         bool
	// Records is the trace of every processed segment, in run order.
	Records []SegmentRecord
	// Cases is the validated pipeline output per test case.
	Cases []harness.CaseOutput
	// CostPerHour is the host price in USD per wall-clock hour, when known.
	CostPerHour *float64
}

// Distribution summarizes samples with nearest-rank percentiles.
type Distribution struct {
	Count   int     `json:"count"`
	P50     float64 `json:"p50"`
	P95     float64 `json:"p95"`
	P99     float64 `json:"p99"`
	Max     float64 `json:"max"`
	TotalMs float64 `json:"totalMs"`
}

// Summary is the per-experiment operational summary written next to the
// ExperimentResult. It carries what the ExperimentResult schema has no field
// for: stage-level timing, queueing, real-time factor and failures.
type Summary struct {
	FormatVersion      string                  `json:"formatVersion"`
	ExperimentID       string                  `json:"experimentId"`
	Pacing             string                  `json:"pacing"`
	TestCases          int                     `json:"testCases"`
	Segments           int                     `json:"segments"`
	FailedSegments     int                     `json:"failedSegments"`
	FailedByStage      map[string]int          `json:"failedByStage"`
	AudioObservations  int                     `json:"audioObservations"`
	VisualObservations int                     `json:"visualObservations"`
	ContextEvents      int                     `json:"contextEvents"`
	MediaMs            int64                   `json:"mediaMs"`
	ProcessingMs       float64                 `json:"processingMs"`
	RealTimeFactor     *float64                `json:"realTimeFactor,omitempty"`
	StagesMs           map[string]Distribution `json:"stagesMs"`
	VisionPerFrameMs   *Distribution           `json:"visionPerFrameMs,omitempty"`
	SegmentMs          Distribution            `json:"segmentMs"`
	// Live only: StartedAt - DueAt, FinishedAt - DueAt and, per ContextEvent,
	// FinishedAt - OccurredAt (the ExperimentResult latency definition).
	QueueWaitMs                  *Distribution `json:"queueWaitMs,omitempty"`
	ProcessingLatencyMs          *Distribution `json:"processingLatencyMs,omitempty"`
	ContextAvailabilityLatencyMs *Distribution `json:"contextAvailabilityLatencyMs,omitempty"`
	Notes                        []string      `json:"notes"`
}

// Metrics computes the ExperimentResult metrics of a finished run. A metric
// with no sample is left absent.
//
//   - latencyP50/P95/P99Ms: live only, one sample per emitted ContextEvent:
//     segment FinishedAt minus the window start on the replay clock.
//   - schemaCompliance: validated contract objects / emitted objects. The
//     runner rejects any invalid object, so a completed run always scores 1.
//   - evidenceTraceability: events whose every evidence item resolves to an
//     observation of the same test case / events.
//   - contextStability: per test case, the mean Jaccard similarity of the
//     label sets of adjacent windows (pairs where both are empty are
//     skipped), averaged over test cases. Labels are compared lexically.
//   - costPerVideoHour: CostPerHour x processing time / media time.
func Metrics(in RunInput) contracts.ExperimentMetrics {
	var m contracts.ExperimentMetrics
	if in.Live {
		if d, ok := distribution(availabilitySamples(in.Records)); ok {
			m.LatencyP50Ms, m.LatencyP95Ms, m.LatencyP99Ms = ptr(d.P50), ptr(d.P95), ptr(d.P99)
		}
	}
	emitted, events, traceable := 0, 0, 0
	for _, c := range in.Cases {
		out := c.Output
		emitted += len(out.AudioObservations) + len(out.VisualObservations) + len(out.ContextEvents)
		catalog, err := contracts.NewCatalog(out.AudioObservations, out.VisualObservations)
		for _, e := range out.ContextEvents {
			events++
			if err == nil && e.ValidateEvidence(catalog) == nil {
				traceable++
			}
		}
	}
	if emitted > 0 {
		m.SchemaCompliance = ptr(1.0)
	}
	if events > 0 {
		m.EvidenceTraceability = ptr(float64(traceable) / float64(events))
	}
	if s, ok := stability(in); ok {
		m.ContextStability = ptr(s)
	}
	if in.CostPerHour != nil {
		if processing, media := processingAndMediaMs(in.Records); media > 0 {
			m.CostPerVideoHour = ptr(*in.CostPerHour * processing / float64(media))
		}
	}
	return m
}

// Summarize builds the operational Summary of a finished run.
func Summarize(in RunInput) Summary {
	s := Summary{
		FormatVersion: SummaryFormatVersion,
		ExperimentID:  in.ExperimentID,
		Pacing:        PacingInstant,
		TestCases:     len(in.Cases),
		Segments:      len(in.Records),
		FailedByStage: map[string]int{},
		StagesMs:      map[string]Distribution{},
		Notes: []string{
			"All confidences in raw artifacts are uncalibrated model self-assessments, not probabilities (ADR-008).",
			"schemaCompliance is 1 by construction for a completed run: invalid output aborts the run.",
		},
	}
	if in.Live {
		s.Pacing = PacingLive
	}
	for _, c := range in.Cases {
		s.AudioObservations += len(c.Output.AudioObservations)
		s.VisualObservations += len(c.Output.VisualObservations)
		s.ContextEvents += len(c.Output.ContextEvents)
	}
	processing, media := processingAndMediaMs(in.Records)
	s.ProcessingMs, s.MediaMs = processing, media
	if media > 0 {
		s.RealTimeFactor = ptr(processing / float64(media))
	}
	stages := map[string][]float64{}
	var perFrame, segment, queue, latency []float64
	for _, r := range in.Records {
		if r.Status == StatusFailed {
			s.FailedSegments++
			stage := r.FailedStage
			if stage == "" {
				stage = "unknown"
			}
			s.FailedByStage[stage]++
		}
		for _, st := range r.Stages {
			stages[st.Stage] = append(stages[st.Stage], st.DurationMs)
			if st.Stage == StageVision && st.Frames > 0 {
				perFrame = append(perFrame, st.DurationMs/float64(st.Frames))
			}
		}
		segment = append(segment, durationMs(r.FinishedAt.Sub(r.StartedAt)))
		queue = append(queue, durationMs(r.StartedAt.Sub(r.DueAt)))
		latency = append(latency, durationMs(r.FinishedAt.Sub(r.DueAt)))
	}
	for name, samples := range stages {
		if d, ok := distribution(samples); ok {
			s.StagesMs[name] = d
		}
	}
	if d, ok := distribution(perFrame); ok {
		s.VisionPerFrameMs = &d
	}
	if d, ok := distribution(segment); ok {
		s.SegmentMs = d
	}
	if in.Live {
		if d, ok := distribution(queue); ok {
			s.QueueWaitMs = &d
		}
		if d, ok := distribution(latency); ok {
			s.ProcessingLatencyMs = &d
		}
		if d, ok := distribution(availabilitySamples(in.Records)); ok {
			s.ContextAvailabilityLatencyMs = &d
		}
	}
	return s
}

func availabilitySamples(records []SegmentRecord) []float64 {
	var samples []float64
	for _, r := range records {
		if r.Status != StatusOK || r.OccurredAt == nil {
			continue
		}
		latency := durationMs(r.FinishedAt.Sub(*r.OccurredAt))
		for range r.ContextEventIDs {
			samples = append(samples, latency)
		}
	}
	return samples
}

func processingAndMediaMs(records []SegmentRecord) (float64, int64) {
	var processing float64
	var media int64
	for _, r := range records {
		processing += durationMs(r.FinishedAt.Sub(r.StartedAt))
		media += r.Window.EndMs - r.Window.StartMs
	}
	return processing, media
}

func stability(in RunInput) (float64, bool) {
	byCase := map[string][]SegmentRecord{}
	for _, r := range in.Records {
		byCase[r.TestCaseID] = append(byCase[r.TestCaseID], r)
	}
	var caseMeans []float64
	for _, c := range in.Cases {
		labels := map[string][]string{}
		for _, e := range c.Output.ContextEvents {
			labels[e.EventID] = eventLabels(e)
		}
		var prev map[string]bool
		var sum float64
		pairs := 0
		for i, r := range byCase[c.TestCaseID] {
			set := map[string]bool{}
			for _, id := range r.ContextEventIDs {
				for _, l := range labels[id] {
					set[l] = true
				}
			}
			if i > 0 && (len(prev) > 0 || len(set) > 0) {
				sum += jaccard(prev, set)
				pairs++
			}
			prev = set
		}
		if pairs > 0 {
			caseMeans = append(caseMeans, sum/float64(pairs))
		}
	}
	if len(caseMeans) == 0 {
		return 0, false
	}
	var total float64
	for _, v := range caseMeans {
		total += v
	}
	return total / float64(len(caseMeans)), true
}

func eventLabels(e contracts.ContextEventV1) []string {
	var labels []string
	for _, v := range e.Context.Entities {
		labels = append(labels, "entity:"+v.Type+":"+v.Value)
	}
	for _, v := range e.Context.Topics {
		labels = append(labels, "topic:"+v.Value)
	}
	for _, v := range e.Context.Objects {
		labels = append(labels, "object:"+v.Value)
	}
	for _, v := range e.Context.Brands {
		labels = append(labels, "brand:"+v.Value)
	}
	return labels
}

func jaccard(a, b map[string]bool) float64 {
	union := len(a)
	inter := 0
	for k := range b {
		if a[k] {
			inter++
		} else {
			union++
		}
	}
	if union == 0 {
		return 1
	}
	return float64(inter) / float64(union)
}

// distribution returns nearest-rank percentiles; ok is false without samples.
func distribution(samples []float64) (Distribution, bool) {
	if len(samples) == 0 {
		return Distribution{}, false
	}
	sorted := slices.Clone(samples)
	sort.Float64s(sorted)
	rank := func(p float64) float64 {
		k := int(math.Ceil(p / 100 * float64(len(sorted))))
		if k < 1 {
			k = 1
		}
		return sorted[k-1]
	}
	var total float64
	for _, v := range sorted {
		total += v
	}
	return Distribution{
		Count:   len(sorted),
		P50:     rank(50),
		P95:     rank(95),
		P99:     rank(99),
		Max:     sorted[len(sorted)-1],
		TotalMs: total,
	}, true
}

func ptr(v float64) *float64 { return &v }
