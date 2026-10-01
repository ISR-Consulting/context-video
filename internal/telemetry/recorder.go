package telemetry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ISR-Consulting/context-video/internal/audio"
	contextcore "github.com/ISR-Consulting/context-video/internal/context"
	"github.com/ISR-Consulting/context-video/internal/media"
	"github.com/ISR-Consulting/context-video/internal/vision"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// TraceFormatVersion identifies the SegmentRecord JSON shape. It is an
// internal evaluation artifact, not a versioned contract under specs/.
const TraceFormatVersion = "m08-trace-1"

// Pacing names recorded in traces and summaries.
const (
	PacingInstant = "instant"
	PacingLive    = "live"
)

// Record statuses.
const (
	StatusOK     = "ok"
	StatusFailed = "failed"
)

// Stage names of timed port calls.
const (
	StageAudio  = "audio"
	StageVision = "vision"
	StageReason = "reason"
)

// StageTiming is one timed port call made while processing a segment.
type StageTiming struct {
	Stage      string  `json:"stage"`
	DurationMs float64 `json:"durationMs"`
	// Frames is the number of frames analyzed by a vision call.
	Frames int  `json:"frames,omitempty"`
	Failed bool `json:"failed,omitempty"`
}

// SegmentRecord is the trace of one processed segment. All times are wall
// times. OccurredAt is set only for live pacing: it is when the window's
// first media instant played on the replay clock (t0 + startMs/speed).
type SegmentRecord struct {
	FormatVersion   string               `json:"formatVersion"`
	ExperimentID    string               `json:"experimentId"`
	TestCaseID      string               `json:"testCaseId"`
	SegmentID       string               `json:"segmentId"`
	Window          contracts.TimeWindow `json:"window"`
	Pacing          string               `json:"pacing"`
	ReplayStartedAt time.Time            `json:"replayStartedAt"`
	OccurredAt      *time.Time           `json:"occurredAt,omitempty"`
	DueAt           time.Time            `json:"dueAt"`
	StartedAt       time.Time            `json:"startedAt"`
	FinishedAt      time.Time            `json:"finishedAt"`
	Stages          []StageTiming        `json:"stages"`
	ContextEventIDs []string             `json:"contextEventIds"`
	Status          string               `json:"status"`
	FailedStage     string               `json:"failedStage,omitempty"`
	Error           string               `json:"error,omitempty"`
}

// RecorderConfig configures a Recorder.
type RecorderConfig struct {
	ExperimentID string
	// Clock paces the replay and timestamps records; nil means media.SystemClock.
	Clock media.Clock
	// Pacing selects instant (VOD-like) or live replay of every test case.
	Pacing media.Pacing
	// Sink, when set, receives every record as soon as its segment finishes.
	Sink func(SegmentRecord) error
	// Progress, when set, gets one human-readable line per segment.
	Progress io.Writer
}

// Recorder paces segments through the M04 simulator and records a trace of
// every segment. It implements pipeline.SegmentObserver and wraps the
// perception and reasoning ports to time their calls. It assumes the
// sequential, one-segment-at-a-time execution of the multimodal pipeline and
// is not safe for concurrent use.
type Recorder struct {
	cfg        RecorderConfig
	sim        *media.Simulator
	stepper    *media.Stepper
	testCaseID string
	caseTotal  int
	caseIndex  int
	current    *SegmentRecord
	records    []SegmentRecord
	sinkErr    error
}

// NewRecorder returns a Recorder for cfg.
func NewRecorder(cfg RecorderConfig) (*Recorder, error) {
	if cfg.Clock == nil {
		cfg.Clock = media.SystemClock{}
	}
	sim, err := media.NewSimulator(cfg.Clock, cfg.Pacing)
	if err != nil {
		return nil, err
	}
	return &Recorder{cfg: cfg, sim: sim}, nil
}

// SetSink replaces the record sink. It must be called before the run starts.
func (r *Recorder) SetSink(sink func(SegmentRecord) error) { r.cfg.Sink = sink }

// Live reports whether segments are paced in media time.
func (r *Recorder) Live() bool { return !r.cfg.Pacing.Instant }

func (r *Recorder) pacingName() string {
	if r.Live() {
		return PacingLive
	}
	return PacingInstant
}

// BeginCase starts the replay schedule of one test case at media time 0.
func (r *Recorder) BeginCase(_ context.Context, testCaseID string, segments []contracts.MediaSegment) error {
	if r.sinkErr != nil {
		return r.sinkErr
	}
	stepper, err := r.sim.Start(segments)
	if err != nil {
		return err
	}
	r.stepper, r.testCaseID, r.caseTotal, r.caseIndex = stepper, testCaseID, len(segments), 0
	return nil
}

// BeforeSegment waits until segment is due and opens its record.
func (r *Recorder) BeforeSegment(ctx context.Context, segment contracts.MediaSegment) error {
	if r.sinkErr != nil {
		return r.sinkErr
	}
	if r.stepper == nil {
		return errors.New("telemetry: segment before BeginCase")
	}
	emission, err := r.stepper.Next(ctx)
	if err != nil {
		return err
	}
	if emission.Segment.SegmentID != segment.SegmentID {
		return fmt.Errorf("telemetry: replay released segment %s but the pipeline is processing %s",
			emission.Segment.SegmentID, segment.SegmentID)
	}
	t0 := r.stepper.StartedAt()
	record := SegmentRecord{
		FormatVersion:   TraceFormatVersion,
		ExperimentID:    r.cfg.ExperimentID,
		TestCaseID:      r.testCaseID,
		SegmentID:       segment.SegmentID,
		Window:          segment.Window,
		Pacing:          r.pacingName(),
		ReplayStartedAt: t0.UTC(),
		DueAt:           emission.DueAt.UTC(),
		StartedAt:       r.cfg.Clock.Now().UTC(),
		Stages:          []StageTiming{},
		ContextEventIDs: []string{},
	}
	if r.Live() {
		occurred := t0.Add(time.Duration(float64(segment.Window.StartMs) * float64(time.Millisecond) / r.cfg.Pacing.Speed)).UTC()
		record.OccurredAt = &occurred
	}
	r.current = &record
	return nil
}

// AfterSegment closes the segment's record and hands it to the sink.
func (r *Recorder) AfterSegment(segment contracts.MediaSegment, events []contracts.ContextEventV1, err error) {
	record := r.current
	r.current = nil
	if record == nil || record.SegmentID != segment.SegmentID {
		return
	}
	record.FinishedAt = r.cfg.Clock.Now().UTC()
	for _, e := range events {
		record.ContextEventIDs = append(record.ContextEventIDs, e.EventID)
	}
	record.Status = StatusOK
	if err != nil {
		record.Status = StatusFailed
		record.Error = err.Error()
		var staged *contextcore.Error
		if errors.As(err, &staged) {
			record.FailedStage = string(staged.Stage)
		}
	}
	r.records = append(r.records, *record)
	r.caseIndex++
	if r.cfg.Sink != nil && r.sinkErr == nil {
		if sinkErr := r.cfg.Sink(*record); sinkErr != nil {
			r.sinkErr = fmt.Errorf("telemetry: write trace: %w", sinkErr)
		}
	}
	if r.cfg.Progress != nil {
		status := record.Status
		if record.FailedStage != "" {
			status += " at " + record.FailedStage
		}
		fmt.Fprintf(r.cfg.Progress, "%s %s segment %d/%d %s: %s, %d events, %.1fs\n",
			r.cfg.ExperimentID, r.testCaseID, r.caseIndex, r.caseTotal, record.SegmentID, status,
			len(record.ContextEventIDs), record.FinishedAt.Sub(record.StartedAt).Seconds())
	}
}

// Records returns the records of every finished segment, in order.
func (r *Recorder) Records() []SegmentRecord {
	return append([]SegmentRecord(nil), r.records...)
}

// Err reports a trace sink failure.
func (r *Recorder) Err() error { return r.sinkErr }

func (r *Recorder) observe(segmentID, stage string, start time.Time, frames int, err error) {
	if r.current == nil || r.current.SegmentID != segmentID {
		return
	}
	r.current.Stages = append(r.current.Stages, StageTiming{
		Stage:      stage,
		DurationMs: durationMs(r.cfg.Clock.Now().Sub(start)),
		Frames:     frames,
		Failed:     err != nil,
	})
}

// Transcriber times every call of inner.
func (r *Recorder) Transcriber(inner audio.Transcriber) audio.Transcriber {
	return audio.TranscriberFunc(func(ctx context.Context, req audio.Request) (audio.Transcription, error) {
		start := r.cfg.Clock.Now()
		out, err := inner.Transcribe(ctx, req)
		r.observe(req.Segment.SegmentID, StageAudio, start, 0, err)
		return out, err
	})
}

// Analyzer times every call of inner.
func (r *Recorder) Analyzer(inner vision.Analyzer) vision.Analyzer {
	return vision.AnalyzerFunc(func(ctx context.Context, req vision.Request) (vision.Analysis, error) {
		start := r.cfg.Clock.Now()
		out, err := inner.Analyze(ctx, req)
		r.observe(req.Segment.SegmentID, StageVision, start, len(req.FrameTimesMs), err)
		return out, err
	})
}

// Reasoner times every call of inner.
func (r *Recorder) Reasoner(inner contextcore.Reasoner) contextcore.Reasoner {
	return contextcore.ReasonerFunc(func(ctx context.Context, group contextcore.EvidenceGroup) ([]contextcore.Candidate, error) {
		start := r.cfg.Clock.Now()
		out, err := inner.Reason(ctx, group)
		r.observe(group.SegmentID, StageReason, start, 0, err)
		return out, err
	})
}

func durationMs(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
