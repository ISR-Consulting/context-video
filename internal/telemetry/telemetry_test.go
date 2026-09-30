package telemetry

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ISR-Consulting/context-video/internal/audio"
	contextcore "github.com/ISR-Consulting/context-video/internal/context"
	"github.com/ISR-Consulting/context-video/internal/evaluation/harness"
	"github.com/ISR-Consulting/context-video/internal/media"
	"github.com/ISR-Consulting/context-video/internal/vision"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// fakeClock advances only on Sleep and Advance.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.Advance(d)
	return nil
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

var content = contracts.ContentRef{ContentID: "clip", ContentType: contracts.ContentTypeLive}

func segments(t *testing.T, durationMs int64, window time.Duration) []contracts.MediaSegment {
	t.Helper()
	s, err := media.Segment(media.Source{Content: content, URI: "media/clip.mp4", DurationMs: durationMs}, window)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func event(segment contracts.MediaSegment, n int, topics ...string) contracts.ContextEventV1 {
	e := contracts.ContextEventV1{
		EventID: fmt.Sprintf("ctx:%s:%d", segment.SegmentID, n), SchemaVersion: "1.0",
		Content: segment.Content, Window: segment.Window, Confidence: 0.5,
		Evidence: contracts.Evidence{Audio: []contracts.AudioEvidence{{
			ObservationID: "aud:" + segment.SegmentID, StartMs: segment.Window.StartMs, EndMs: segment.Window.EndMs, Text: "x",
		}}, Visual: []contracts.VisualEvidence{}},
	}
	for _, topic := range topics {
		e.Context.Topics = append(e.Context.Topics, contracts.SemanticValue{Value: topic, Confidence: 0.5})
	}
	return e
}

func observation(segment contracts.MediaSegment) contracts.AudioObservation {
	return contracts.AudioObservation{
		ObservationID: "aud:" + segment.SegmentID, Content: segment.Content, Window: segment.Window,
		Transcript: contracts.Transcript{Text: "x"}, Provenance: contracts.ObservationProvenance{Provider: "fake", PipelineVersion: "poc-v1"},
	}
}

// runSegments drives rec like the multimodal pipeline would, spending the
// given processing time inside a timed transcriber call per segment.
func runSegments(t *testing.T, rec *Recorder, clock *fakeClock, segs []contracts.MediaSegment, work []time.Duration, fail map[int]error) []contracts.ContextEventV1 {
	t.Helper()
	ctx := context.Background()
	transcriber := rec.Transcriber(audio.TranscriberFunc(func(_ context.Context, req audio.Request) (audio.Transcription, error) {
		for i, s := range segs {
			if s.SegmentID == req.Segment.SegmentID {
				clock.Advance(work[i])
				if err := fail[i]; err != nil {
					return audio.Transcription{}, err
				}
			}
		}
		return audio.Transcription{Provider: "fake"}, nil
	}))
	if err := rec.BeginCase(ctx, "case-1", segs); err != nil {
		t.Fatal(err)
	}
	var events []contracts.ContextEventV1
	for _, s := range segs {
		if err := rec.BeforeSegment(ctx, s); err != nil {
			t.Fatal(err)
		}
		if _, err := transcriber.Transcribe(ctx, audio.Request{Segment: s}); err != nil {
			rec.AfterSegment(s, nil, contextcore.SegmentError(contextcore.StageAudio, s, "fake", err))
			continue
		}
		e := event(s, 1, "football")
		events = append(events, e)
		rec.AfterSegment(s, []contracts.ContextEventV1{e}, nil)
	}
	return events
}

func TestRecorderLivePacingAndLatency(t *testing.T) {
	clock := newFakeClock()
	var sunk []SegmentRecord
	var progress strings.Builder
	rec, err := NewRecorder(RecorderConfig{
		ExperimentID: "E01", Clock: clock, Pacing: media.Pacing{Speed: 1},
		Sink: func(r SegmentRecord) error { sunk = append(sunk, r); return nil }, Progress: &progress,
	})
	if err != nil {
		t.Fatal(err)
	}
	t0 := clock.Now()
	segs := segments(t, 10000, 5*time.Second)
	events := runSegments(t, rec, clock, segs, []time.Duration{2 * time.Second, 7 * time.Second}, nil)

	records := rec.Records()
	if len(records) != 2 || len(sunk) != 2 {
		t.Fatalf("records %d sunk %d", len(records), len(sunk))
	}
	first, second := records[0], records[1]
	if !first.DueAt.Equal(t0.Add(5*time.Second)) || !first.StartedAt.Equal(first.DueAt) || !first.FinishedAt.Equal(t0.Add(7*time.Second)) ||
		first.OccurredAt == nil || !first.OccurredAt.Equal(t0) || first.Pacing != PacingLive {
		t.Fatalf("first record %+v", first)
	}
	if !second.StartedAt.Equal(t0.Add(10*time.Second)) || !second.FinishedAt.Equal(t0.Add(17*time.Second)) ||
		!second.OccurredAt.Equal(t0.Add(5*time.Second)) {
		t.Fatalf("second record %+v", second)
	}
	if len(first.Stages) != 1 || first.Stages[0].Stage != StageAudio || first.Stages[0].DurationMs != 2000 {
		t.Fatalf("stages %+v", first.Stages)
	}
	if first.FormatVersion != TraceFormatVersion || first.ExperimentID != "E01" || first.TestCaseID != "case-1" ||
		fmt.Sprint(first.ContextEventIDs) != "[ctx:clip:0-5000:1]" || first.Status != StatusOK {
		t.Fatalf("identity %+v", first)
	}
	if !strings.Contains(progress.String(), "E01 case-1 segment 2/2 clip:5000-10000: ok, 1 events, 7.0s") {
		t.Fatalf("progress %q", progress.String())
	}

	in := RunInput{ExperimentID: "E01", Live: true, Records: records, Cases: []harness.CaseOutput{{
		TestCaseID: "case-1",
		Output:     harness.PipelineOutput{AudioObservations: []contracts.AudioObservation{observation(segs[0]), observation(segs[1])}, ContextEvents: events},
	}}}
	m := Metrics(in)
	if *m.LatencyP50Ms != 7000 || *m.LatencyP95Ms != 12000 || *m.LatencyP99Ms != 12000 {
		t.Fatalf("latency %v %v %v", *m.LatencyP50Ms, *m.LatencyP95Ms, *m.LatencyP99Ms)
	}
	if *m.SchemaCompliance != 1 || *m.EvidenceTraceability != 1 || *m.ContextStability != 1 || m.CostPerVideoHour != nil {
		t.Fatalf("metrics %+v", m)
	}
	s := Summarize(in)
	if s.Pacing != PacingLive || s.QueueWaitMs == nil || s.QueueWaitMs.Max != 0 || s.ProcessingLatencyMs.Max != 7000 ||
		*s.RealTimeFactor != 0.9 || s.StagesMs[StageAudio].TotalMs != 9000 || s.ContextAvailabilityLatencyMs.Count != 2 {
		t.Fatalf("summary %+v", s)
	}
}

func TestRecorderInstantPacingHasNoLatency(t *testing.T) {
	clock := newFakeClock()
	rec, err := NewRecorder(RecorderConfig{ExperimentID: "E04", Clock: clock, Pacing: media.Pacing{Instant: true}})
	if err != nil {
		t.Fatal(err)
	}
	segs := segments(t, 10000, 5*time.Second)
	events := runSegments(t, rec, clock, segs, []time.Duration{time.Second, time.Second}, nil)
	records := rec.Records()
	if records[0].OccurredAt != nil || records[0].Pacing != PacingInstant || !records[1].StartedAt.Equal(records[0].FinishedAt) {
		t.Fatalf("instant records %+v", records)
	}
	price := 3.0
	in := RunInput{Records: records, CostPerHour: &price, Cases: []harness.CaseOutput{{TestCaseID: "case-1",
		Output: harness.PipelineOutput{AudioObservations: []contracts.AudioObservation{observation(segs[0]), observation(segs[1])}, ContextEvents: events}}}}
	m := Metrics(in)
	if m.LatencyP50Ms != nil || m.LatencyP95Ms != nil || m.LatencyP99Ms != nil {
		t.Fatalf("instant run reported latency %+v", m)
	}
	// 2 s of processing for 10 s of media at 3 USD/h.
	if math.Abs(*m.CostPerVideoHour-0.6) > 1e-9 {
		t.Fatalf("cost %v", *m.CostPerVideoHour)
	}
	if s := Summarize(in); s.QueueWaitMs != nil || s.ContextAvailabilityLatencyMs != nil {
		t.Fatalf("instant summary has live-only fields %+v", s)
	}
}

func TestRecorderRecordsFailuresAndSinkErrors(t *testing.T) {
	clock := newFakeClock()
	sinkErr := errors.New("disk full")
	calls := 0
	rec, err := NewRecorder(RecorderConfig{ExperimentID: "E01", Clock: clock, Pacing: media.Pacing{Instant: true},
		Sink: func(SegmentRecord) error { calls++; return sinkErr }})
	if err != nil {
		t.Fatal(err)
	}
	segs := segments(t, 10000, 5*time.Second)
	ctx := context.Background()
	if err := rec.BeginCase(ctx, "case-1", segs); err != nil {
		t.Fatal(err)
	}
	if err := rec.BeforeSegment(ctx, segs[0]); err != nil {
		t.Fatal(err)
	}
	rec.AfterSegment(segs[0], nil, contextcore.SegmentError(contextcore.StageReason, segs[0], "llama-cpp", errors.New("malformed")))
	r := rec.Records()[0]
	if r.Status != StatusFailed || r.FailedStage != "reason" || !strings.Contains(r.Error, "malformed") {
		t.Fatalf("failed record %+v", r)
	}
	if err := rec.BeforeSegment(ctx, segs[1]); !errors.Is(err, sinkErr) || !errors.Is(rec.Err(), sinkErr) || calls != 1 {
		t.Fatalf("sink error not surfaced: %v", err)
	}
	s := Summarize(RunInput{Records: rec.Records()})
	if s.FailedSegments != 1 || s.FailedByStage["reason"] != 1 {
		t.Fatalf("summary failures %+v", s)
	}
}

func TestRecorderRejectsOutOfOrderSegmentsAndCancellation(t *testing.T) {
	rec, err := NewRecorder(RecorderConfig{Clock: newFakeClock(), Pacing: media.Pacing{Speed: 1}})
	if err != nil {
		t.Fatal(err)
	}
	segs := segments(t, 10000, 5*time.Second)
	if err := rec.BeforeSegment(context.Background(), segs[0]); err == nil {
		t.Fatal("segment before BeginCase accepted")
	}
	if err := rec.BeginCase(context.Background(), "c", segs); err != nil {
		t.Fatal(err)
	}
	if err := rec.BeforeSegment(context.Background(), segs[1]); err == nil {
		t.Fatal("out-of-order segment accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := rec.BeforeSegment(ctx, segs[1]); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled wait: %v", err)
	}
}

func TestDecoratorsTimeCallsAndPassErrorsThrough(t *testing.T) {
	clock := newFakeClock()
	rec, err := NewRecorder(RecorderConfig{Clock: clock, Pacing: media.Pacing{Instant: true}})
	if err != nil {
		t.Fatal(err)
	}
	segs := segments(t, 5000, 5*time.Second)
	ctx := context.Background()
	if err := rec.BeginCase(ctx, "c", segs); err != nil {
		t.Fatal(err)
	}
	if err := rec.BeforeSegment(ctx, segs[0]); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("vlm crashed")
	analyzer := rec.Analyzer(vision.AnalyzerFunc(func(context.Context, vision.Request) (vision.Analysis, error) {
		clock.Advance(3 * time.Second)
		return vision.Analysis{}, cause
	}))
	if _, err := analyzer.Analyze(ctx, vision.Request{Segment: segs[0], FrameTimesMs: []int64{1250, 3750}}); !errors.Is(err, cause) {
		t.Fatalf("analyzer error %v", err)
	}
	reasoner := rec.Reasoner(contextcore.ReasonerFunc(func(ctx context.Context, g contextcore.EvidenceGroup) ([]contextcore.Candidate, error) {
		clock.Advance(time.Second)
		return nil, ctx.Err()
	}))
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := reasoner.Reason(cctx, contextcore.EvidenceGroup{SegmentID: segs[0].SegmentID}); !errors.Is(err, context.Canceled) {
		t.Fatalf("reasoner error %v", err)
	}
	rec.AfterSegment(segs[0], nil, nil)
	stages := rec.Records()[0].Stages
	want := []StageTiming{{Stage: StageVision, DurationMs: 3000, Frames: 2, Failed: true}, {Stage: StageReason, DurationMs: 1000, Failed: true}}
	if fmt.Sprint(stages) != fmt.Sprint(want) {
		t.Fatalf("stages %+v", stages)
	}
	if d := Summarize(RunInput{Records: rec.Records()}); d.VisionPerFrameMs == nil || d.VisionPerFrameMs.P50 != 1500 {
		t.Fatalf("per-frame %+v", d.VisionPerFrameMs)
	}
}

func TestDistributionNearestRank(t *testing.T) {
	if _, ok := distribution(nil); ok {
		t.Fatal("empty distribution reported")
	}
	var samples []float64
	for i := 100; i >= 1; i-- {
		samples = append(samples, float64(i))
	}
	d, _ := distribution(samples)
	if d.P50 != 50 || d.P95 != 95 || d.P99 != 99 || d.Max != 100 || d.Count != 100 || d.TotalMs != 5050 {
		t.Fatalf("distribution %+v", d)
	}
	one, _ := distribution([]float64{42})
	if one.P50 != 42 || one.P99 != 42 {
		t.Fatalf("single sample %+v", one)
	}
}

func TestStabilityAndTraceability(t *testing.T) {
	segs := segments(t, 20000, 5*time.Second)
	rec := func(i int, events ...contracts.ContextEventV1) SegmentRecord {
		r := SegmentRecord{TestCaseID: "c", SegmentID: segs[i].SegmentID, Window: segs[i].Window, Status: StatusOK, ContextEventIDs: []string{}}
		for _, e := range events {
			r.ContextEventIDs = append(r.ContextEventIDs, e.EventID)
		}
		return r
	}
	e0 := event(segs[0], 1, "football", "goal")
	e1 := event(segs[1], 1, "football")
	e3 := event(segs[3], 1, "cooking")
	e3.Evidence.Audio[0].ObservationID = "aud:unknown"
	// Pairs: {football,goal}->{football} = 0.5; {football}->{} = 0; {}->{cooking} = 0.
	in := RunInput{
		Records: []SegmentRecord{rec(0, e0), rec(1, e1), rec(2), rec(3, e3)},
		Cases: []harness.CaseOutput{{TestCaseID: "c", Output: harness.PipelineOutput{
			AudioObservations: []contracts.AudioObservation{observation(segs[0]), observation(segs[1]), observation(segs[2]), observation(segs[3])},
			ContextEvents:     []contracts.ContextEventV1{e0, e1, e3},
		}}},
	}
	m := Metrics(in)
	if math.Abs(*m.ContextStability-0.5/3) > 1e-9 {
		t.Fatalf("stability %v", *m.ContextStability)
	}
	if math.Abs(*m.EvidenceTraceability-2.0/3) > 1e-9 {
		t.Fatalf("traceability %v", *m.EvidenceTraceability)
	}

	empty := Metrics(RunInput{Records: []SegmentRecord{rec(0), rec(1)}, Cases: []harness.CaseOutput{{TestCaseID: "c"}}})
	if empty.ContextStability != nil || empty.EvidenceTraceability != nil || empty.SchemaCompliance != nil {
		t.Fatalf("metrics without samples %+v", empty)
	}
	single := Metrics(RunInput{Records: []SegmentRecord{rec(0, e0)}, Cases: []harness.CaseOutput{{TestCaseID: "c",
		Output: harness.PipelineOutput{ContextEvents: []contracts.ContextEventV1{e0}}}}})
	if single.ContextStability != nil {
		t.Fatalf("single window stability %v", *single.ContextStability)
	}
}
