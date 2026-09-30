package media_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/ISR-Consulting/context-video/internal/media"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

func TestStepperReleasesOnScheduleAndReportsLateness(t *testing.T) {
	clock := newFakeClock()
	sim, err := media.NewSimulator(clock, media.Pacing{Speed: 1})
	if err != nil {
		t.Fatal(err)
	}
	segments := segmentsFor(t, 12000, 5*time.Second)
	stepper, err := sim.Start(segments)
	if err != nil {
		t.Fatal(err)
	}
	t0 := stepper.StartedAt()
	if !t0.Equal(clock.Now()) {
		t.Fatalf("t0 %v, want %v", t0, clock.Now())
	}

	first, err := stepper.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Sequence != 0 || !first.DueAt.Equal(t0.Add(5*time.Second)) || !first.EmittedAt.Equal(first.DueAt) {
		t.Fatalf("first emission %+v", first)
	}

	// The consumer spends 8 s on the first segment: the second (due at 10 s)
	// is released immediately on the next pull, 3 s late.
	clock.Advance(8 * time.Second)
	second, err := stepper.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !second.DueAt.Equal(t0.Add(10*time.Second)) || second.EmittedAt.Sub(second.DueAt) != 3*time.Second {
		t.Fatalf("second emission due %v emitted %v", second.DueAt.Sub(t0), second.EmittedAt.Sub(t0))
	}

	third, err := stepper.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if third.Segment.Window.EndMs != 12000 || !third.DueAt.Equal(t0.Add(12*time.Second)) {
		t.Fatalf("third emission %+v", third)
	}
	if _, err := stepper.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("after last segment: %v", err)
	}
}

func TestStepperInstantAndCancellation(t *testing.T) {
	clock := newFakeClock()
	sim, err := media.NewSimulator(clock, media.Pacing{Instant: true})
	if err != nil {
		t.Fatal(err)
	}
	stepper, err := sim.Start(segmentsFor(t, 10000, 5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	e, err := stepper.Next(context.Background())
	if err != nil || !e.DueAt.Equal(stepper.StartedAt()) {
		t.Fatalf("instant emission %+v, %v", e, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = stepper.Next(ctx)
	var mediaErr *media.Error
	if !errors.As(err, &mediaErr) || mediaErr.Stage != media.StageCancelled || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled pull: %v", err)
	}
	if len(clock.Sleeps()) != 0 {
		t.Fatalf("instant pacing slept: %v", clock.Sleeps())
	}
}

func TestStepperRejectsUnorderedSegments(t *testing.T) {
	sim, err := media.NewSimulator(newFakeClock(), media.Pacing{Speed: 1})
	if err != nil {
		t.Fatal(err)
	}
	segments := segmentsFor(t, 10000, 5*time.Second)
	if _, err := sim.Start([]contracts.MediaSegment{segments[1], segments[0]}); err == nil {
		t.Fatal("unordered segments accepted")
	}
}
