package media_test

import (
	"context"
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/ISR-Consulting/context-video/internal/media"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

func segmentsFor(t *testing.T, durationMs int64, window time.Duration) []contracts.MediaSegment {
	t.Helper()
	segments, err := media.Segment(source(contracts.ContentTypeLive, durationMs), window)
	if err != nil {
		t.Fatal(err)
	}
	return segments
}

func collect(t *testing.T, sim *media.Simulator, segments []contracts.MediaSegment) []media.Emission {
	t.Helper()
	var got []media.Emission
	n, err := sim.Replay(context.Background(), segments, func(e media.Emission) error {
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != len(segments) || len(got) != len(segments) {
		t.Fatalf("emitted %d (callback %d), want %d", n, len(got), len(segments))
	}
	for i, e := range got {
		if e.Sequence != i || e.Segment.SegmentID != segments[i].SegmentID {
			t.Fatalf("emission %d = sequence %d segment %s", i, e.Sequence, e.Segment.SegmentID)
		}
	}
	return got
}

func TestReplayPacing(t *testing.T) {
	cases := []struct {
		name       string
		speed      float64
		durationMs int64
		want       []time.Duration
	}{
		{"real time", 1, 10000, []time.Duration{5 * time.Second, 5 * time.Second}},
		{"ten times faster", 10, 10000, []time.Duration{500 * time.Millisecond, 500 * time.Millisecond}},
		{"half speed", 0.5, 10000, []time.Duration{10 * time.Second, 10 * time.Second}},
		{"partial last window", 1, 8000, []time.Duration{5 * time.Second, 3 * time.Second}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clock := newFakeClock()
			t0 := clock.Now()
			sim, err := media.NewSimulator(clock, media.Pacing{Speed: tc.speed})
			if err != nil {
				t.Fatal(err)
			}
			segments := segmentsFor(t, tc.durationMs, 5*time.Second)
			got := collect(t, sim, segments)
			if sleeps := clock.Sleeps(); !slices.Equal(sleeps, tc.want) {
				t.Fatalf("sleeps = %v, want %v", sleeps, tc.want)
			}
			for i, e := range got {
				wantDue := t0.Add(time.Duration(float64(segments[i].Window.EndMs) * float64(time.Millisecond) / tc.speed))
				if !e.DueAt.Equal(wantDue) || !e.EmittedAt.Equal(wantDue) {
					t.Fatalf("emission %d due %v emitted %v, want both %v", i, e.DueAt, e.EmittedAt, wantDue)
				}
			}
		})
	}
}

func TestReplayInstant(t *testing.T) {
	clock := newFakeClock()
	t0 := clock.Now()
	sim, err := media.NewSimulator(clock, media.Pacing{Instant: true, Speed: 0})
	if err != nil {
		t.Fatal(err)
	}
	got := collect(t, sim, segmentsFor(t, 10000, 2*time.Second))
	if sleeps := clock.Sleeps(); len(sleeps) != 0 {
		t.Fatalf("instant replay slept: %v", sleeps)
	}
	for i, e := range got {
		if !e.DueAt.Equal(t0) || !e.EmittedAt.Equal(t0) {
			t.Fatalf("emission %d due %v emitted %v, want %v", i, e.DueAt, e.EmittedAt, t0)
		}
	}
}

func TestReplaySlowConsumerIsNotDroppedOrReordered(t *testing.T) {
	clock := newFakeClock()
	t0 := clock.Now()
	sim, err := media.NewSimulator(clock, media.Pacing{Speed: 1})
	if err != nil {
		t.Fatal(err)
	}
	segments := segmentsFor(t, 15000, 5*time.Second)
	var got []media.Emission
	n, err := sim.Replay(context.Background(), segments, func(e media.Emission) error {
		got = append(got, e)
		if e.Sequence == 0 {
			clock.Advance(12 * time.Second)
		}
		return nil
	})
	if err != nil || n != 3 {
		t.Fatalf("n = %d, err = %v", n, err)
	}
	// Segment 0 sleeps 5s; the consumer then takes 12s (now t0+17s), so
	// segment 1 (due t0+10s) is late and segment 2 (due t0+15s) is late too.
	if sleeps := clock.Sleeps(); !slices.Equal(sleeps, []time.Duration{5 * time.Second}) {
		t.Fatalf("sleeps = %v", sleeps)
	}
	for i, e := range got {
		if e.Sequence != i || e.Segment.SegmentID != segments[i].SegmentID {
			t.Fatalf("emission %d out of order: %+v", i, e)
		}
	}
	for _, i := range []int{1, 2} {
		if !got[i].EmittedAt.After(got[i].DueAt) {
			t.Fatalf("emission %d emitted %v, due %v: want late", i, got[i].EmittedAt, got[i].DueAt)
		}
		if !got[i].EmittedAt.Equal(t0.Add(17 * time.Second)) {
			t.Fatalf("emission %d emitted %v, want t0+17s", i, got[i].EmittedAt)
		}
	}
}

func TestNewSimulatorRejectsInvalidPacing(t *testing.T) {
	for _, speed := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := media.NewSimulator(newFakeClock(), media.Pacing{Speed: speed})
		var mediaErr *media.Error
		if !errors.Is(err, media.ErrInvalidPacing) || !errors.As(err, &mediaErr) || mediaErr.Stage != media.StagePacing {
			t.Fatalf("speed %v: expected pacing error, got %v", speed, err)
		}
	}
	if _, err := media.NewSimulator(nil, media.Pacing{Speed: 1}); err == nil {
		t.Fatal("expected nil clock error")
	}
}

func TestReplayCancelledBeforeStart(t *testing.T) {
	clock := newFakeClock()
	sim, err := media.NewSimulator(clock, media.Pacing{Speed: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	n, err := sim.Replay(ctx, segmentsFor(t, 10000, 5*time.Second), func(media.Emission) error {
		calls++
		return nil
	})
	if n != 0 || calls != 0 {
		t.Fatalf("emitted %d (callback %d) after cancellation", n, calls)
	}
	assertCancelled(t, err, context.Canceled)
}

func TestReplayCancelledDuringEmit(t *testing.T) {
	for _, pacing := range []media.Pacing{{Instant: true}, {Speed: 1}} {
		for k := 0; k < 3; k++ {
			clock := newFakeClock()
			sim, err := media.NewSimulator(clock, pacing)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			calls := 0
			n, err := sim.Replay(ctx, segmentsFor(t, 20000, 5*time.Second), func(e media.Emission) error {
				calls++
				if e.Sequence == k {
					cancel()
				}
				return nil
			})
			cancel()
			if n != k+1 || calls != k+1 {
				t.Fatalf("pacing %+v k=%d: emitted %d (callback %d), want %d", pacing, k, n, calls, k+1)
			}
			assertCancelled(t, err, context.Canceled)
		}
	}
}

func TestReplaySystemClockDeadline(t *testing.T) {
	sim, err := media.NewSimulator(media.SystemClock{}, media.Pacing{Speed: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	n, err := sim.Replay(ctx, segmentsFor(t, 10000, 5*time.Second), func(media.Emission) error { return nil })
	if n != 0 {
		t.Fatalf("emitted %d before the 5s window elapsed", n)
	}
	assertCancelled(t, err, context.DeadlineExceeded)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("cancellation took %v", elapsed)
	}
}

func TestReplayEmitErrorStops(t *testing.T) {
	sim, err := media.NewSimulator(newFakeClock(), media.Pacing{Instant: true})
	if err != nil {
		t.Fatal(err)
	}
	boom := errors.New("consumer failed")
	calls := 0
	n, err := sim.Replay(context.Background(), segmentsFor(t, 20000, 5*time.Second), func(e media.Emission) error {
		calls++
		if e.Sequence == 1 {
			return boom
		}
		return nil
	})
	if n != 1 || calls != 2 {
		t.Fatalf("emitted %d (callback %d), want 1 (2)", n, calls)
	}
	var mediaErr *media.Error
	if !errors.Is(err, boom) || !errors.As(err, &mediaErr) || mediaErr.Stage != media.StageEmit || mediaErr.SegmentID != "content-1:5000-10000" {
		t.Fatalf("expected emit error for second segment, got %v", err)
	}
}

func TestReplayRejectsInvalidSegments(t *testing.T) {
	segments := segmentsFor(t, 15000, 5*time.Second)
	outOfOrder := []contracts.MediaSegment{segments[1], segments[0]}
	overlapping := []contracts.MediaSegment{segments[0], segments[1]}
	overlapping[1].Window.StartMs = 4000
	invalidWindow := []contracts.MediaSegment{segments[0]}
	invalidWindow[0].Window = contracts.TimeWindow{StartMs: 5000, EndMs: 5000}
	for name, input := range map[string][]contracts.MediaSegment{
		"out of order": outOfOrder, "overlapping": overlapping, "invalid window": invalidWindow,
	} {
		t.Run(name, func(t *testing.T) {
			sim, err := media.NewSimulator(newFakeClock(), media.Pacing{Instant: true})
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			n, err := sim.Replay(context.Background(), input, func(media.Emission) error {
				calls++
				return nil
			})
			var mediaErr *media.Error
			if !errors.As(err, &mediaErr) || mediaErr.Stage != media.StageSegment {
				t.Fatalf("expected segment error, got %v", err)
			}
			if n != 0 || calls != 0 {
				t.Fatalf("emitted %d (callback %d) for invalid input", n, calls)
			}
		})
	}
}

func TestReplayRejectsScheduleOverflow(t *testing.T) {
	sim, err := media.NewSimulator(newFakeClock(), media.Pacing{Speed: 1e-9})
	if err != nil {
		t.Fatal(err)
	}
	_, err = sim.Replay(context.Background(), segmentsFor(t, 10000, 5*time.Second), func(media.Emission) error { return nil })
	if !errors.Is(err, media.ErrInvalidPacing) {
		t.Fatalf("expected pacing overflow error, got %v", err)
	}
}

func TestReplayRejectsNilEmit(t *testing.T) {
	sim, err := media.NewSimulator(newFakeClock(), media.Pacing{Instant: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sim.Replay(context.Background(), nil, nil); err == nil {
		t.Fatal("expected nil emit error")
	}
}

func assertCancelled(t *testing.T, err, want error) {
	t.Helper()
	var mediaErr *media.Error
	if !errors.Is(err, want) || !errors.As(err, &mediaErr) || mediaErr.Stage != media.StageCancelled {
		t.Fatalf("expected cancelled error wrapping %v, got %v", want, err)
	}
}
