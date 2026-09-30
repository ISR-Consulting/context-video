package pipeline

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ISR-Consulting/context-video/internal/audio"
	contextcore "github.com/ISR-Consulting/context-video/internal/context"
	"github.com/ISR-Consulting/context-video/internal/evaluation/harness"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// recordingObserver logs every observer call and can fail the gate.
type recordingObserver struct {
	calls   []string
	errs    []error
	gateErr error
}

func (o *recordingObserver) BeginCase(_ context.Context, testCaseID string, segments []contracts.MediaSegment) error {
	o.calls = append(o.calls, fmt.Sprintf("begin %s %d", testCaseID, len(segments)))
	return nil
}

func (o *recordingObserver) BeforeSegment(_ context.Context, segment contracts.MediaSegment) error {
	o.calls = append(o.calls, "before "+segment.SegmentID)
	return o.gateErr
}

func (o *recordingObserver) AfterSegment(segment contracts.MediaSegment, events []contracts.ContextEventV1, err error) {
	o.calls = append(o.calls, fmt.Sprintf("after %s events=%d failed=%t", segment.SegmentID, len(events), err != nil))
	o.errs = append(o.errs, err)
}

func observerInput(t *testing.T) harness.PipelineInput {
	return harness.PipelineInput{Config: multimodalConfig(t, "5s", true, true, "fake-reasoner"), TestCase: footballCase(t)}
}

func TestMultimodalObserverOrder(t *testing.T) {
	obs := &recordingObserver{}
	m := newMultimodal(t, mmParts{transcriber: &fakeTranscriber{}, analyzer: &fakeAnalyzer{}, opts: []MultimodalOption{WithSegmentObserver(obs)}})
	out, err := m.Process(context.Background(), observerInput(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"begin football-live 2",
		"before live-football-001:0-5000", "after live-football-001:0-5000 events=1 failed=false",
		"before live-football-001:5000-10000", "after live-football-001:5000-10000 events=1 failed=false",
	}
	if fmt.Sprint(obs.calls) != fmt.Sprint(want) {
		t.Fatalf("calls %q\nwant %q", obs.calls, want)
	}
	if len(out.ContextEvents) != 2 {
		t.Fatalf("events %d", len(out.ContextEvents))
	}
}

func TestMultimodalRecordedSegmentErrors(t *testing.T) {
	cause := errors.New("malformed answer")
	reasoner := contextcore.ReasonerFunc(func(ctx context.Context, g contextcore.EvidenceGroup) ([]contextcore.Candidate, error) {
		if g.Window.StartMs == 0 {
			return nil, cause
		}
		return (&citingReasoner{}).Reason(ctx, g)
	})
	obs := &recordingObserver{}
	m := newMultimodal(t, mmParts{
		transcriber: &fakeTranscriber{}, analyzer: &fakeAnalyzer{}, reasoner: reasoner,
		opts: []MultimodalOption{WithSegmentObserver(obs), WithRecordedSegmentErrors()},
	})
	out, err := m.Process(context.Background(), observerInput(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.ContextEvents) != 1 || out.ContextEvents[0].Window.StartMs != 5000 {
		t.Fatalf("events %+v", out.ContextEvents)
	}
	if len(out.AudioObservations) != 2 || len(out.VisualObservations) != 2 {
		t.Fatalf("observations of the failed segment were dropped: %d audio %d visual", len(out.AudioObservations), len(out.VisualObservations))
	}
	var staged *contextcore.Error
	if !errors.As(obs.errs[0], &staged) || staged.Stage != contextcore.StageReason || !errors.Is(obs.errs[0], cause) || obs.errs[1] != nil {
		t.Fatalf("recorded errors %v", obs.errs)
	}
}

func TestMultimodalFailModeNotifiesThenAborts(t *testing.T) {
	obs := &recordingObserver{}
	m := newMultimodal(t, mmParts{
		transcriber: &fakeTranscriber{fail: func(r audio.Request) error { return errors.New("stt down") }},
		opts:        []MultimodalOption{WithSegmentObserver(obs)},
	})
	in := harness.PipelineInput{Config: multimodalConfig(t, "5s", true, false, "fake-reasoner"), TestCase: footballCase(t)}
	if _, err := m.Process(context.Background(), in); err == nil {
		t.Fatal("expected failure")
	}
	if len(obs.calls) != 3 || obs.calls[2] != "after live-football-001:0-5000 events=0 failed=true" {
		t.Fatalf("calls %q", obs.calls)
	}
}

func TestMultimodalGateErrorAborts(t *testing.T) {
	gate := errors.New("replay cancelled")
	transcriber := &fakeTranscriber{}
	obs := &recordingObserver{gateErr: gate}
	m := newMultimodal(t, mmParts{transcriber: transcriber, analyzer: &fakeAnalyzer{},
		opts: []MultimodalOption{WithSegmentObserver(obs), WithRecordedSegmentErrors()}})
	if _, err := m.Process(context.Background(), observerInput(t)); !errors.Is(err, gate) || len(transcriber.requests) != 0 {
		t.Fatalf("err %v, transcriptions %d", err, len(transcriber.requests))
	}
}

func TestMultimodalRecordedErrorsStillStopOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	transcriber := &fakeTranscriber{fail: func(r audio.Request) error {
		cancel()
		return context.Canceled
	}}
	obs := &recordingObserver{}
	m := newMultimodal(t, mmParts{transcriber: transcriber, analyzer: &fakeAnalyzer{},
		opts: []MultimodalOption{WithSegmentObserver(obs), WithRecordedSegmentErrors()}})
	if _, err := m.Process(ctx, observerInput(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
}

func TestRecordedSegmentErrorsNeedObserver(t *testing.T) {
	engine, err := contextcore.NewEngine("fake-reasoner", &citingReasoner{})
	if err != nil {
		t.Fatal(err)
	}
	cfg := MultimodalConfig{Engine: engine, AudioProvider: "fake", Transcriber: &fakeTranscriber{}, AudioResolve: DirResolver(datasetRoot)}
	if _, err := NewMultimodal(cfg, WithRecordedSegmentErrors()); err == nil {
		t.Fatal("record mode without observer accepted")
	}
}
