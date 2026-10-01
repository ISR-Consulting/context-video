package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// Pacing selects how fast media time advances relative to wall time.
type Pacing struct {
	// Instant emits every segment without waiting and ignores Speed.
	Instant bool
	// Speed is the media-time to wall-time ratio: 1 is real time, 10 is ten
	// times faster, 0.5 is half speed. It must be finite and > 0 unless Instant.
	Speed float64
}

// Validate reports whether p can schedule a replay.
func (p Pacing) Validate() error {
	if p.Instant {
		return nil
	}
	if math.IsNaN(p.Speed) || math.IsInf(p.Speed, 0) || p.Speed <= 0 {
		return &Error{Stage: StagePacing, Err: fmt.Errorf("%w: speed %v must be finite and > 0", ErrInvalidPacing, p.Speed)}
	}
	return nil
}

// Emission is one segment delivered by a replay. DueAt is when the segment's
// media time had fully elapsed on the schedule; EmittedAt is when it was
// actually handed to the consumer. EmittedAt after DueAt means it was late.
type Emission struct {
	Sequence  int
	Segment   contracts.MediaSegment
	DueAt     time.Time
	EmittedAt time.Time
}

// Simulator replays segments in media time through an injected Clock.
type Simulator struct {
	clock  Clock
	pacing Pacing
}

// NewSimulator returns a Simulator for clock and pacing.
func NewSimulator(clock Clock, pacing Pacing) (*Simulator, error) {
	if clock == nil {
		return nil, errors.New("media: nil clock")
	}
	if err := pacing.Validate(); err != nil {
		return nil, err
	}
	return &Simulator{clock: clock, pacing: pacing}, nil
}

// Replay emits segments in order on the calling goroutine. With t0 the replay
// start, segment i is due at t0 + endMs_i / Speed, so it is released only once
// its whole window has elapsed and waits never accumulate drift. emit is
// synchronous: a slow consumer delays later segments, which are then emitted
// immediately rather than dropped or reordered. Replay stops on the first emit
// error or when ctx is done, returning the number of segments emitted and an
// error wrapping the cause. Nothing is emitted after cancellation is observed.
func (s *Simulator) Replay(ctx context.Context, segments []contracts.MediaSegment, emit func(Emission) error) (int, error) {
	if emit == nil {
		return 0, errors.New("media: nil emit")
	}
	stepper, err := s.Start(segments)
	if err != nil {
		return 0, err
	}
	emitted := 0
	for {
		emission, err := stepper.Next(ctx)
		if errors.Is(err, io.EOF) {
			return emitted, nil
		}
		if err != nil {
			return emitted, err
		}
		if err := emit(emission); err != nil {
			return emitted, &Error{Stage: StageEmit, SegmentID: emission.Segment.SegmentID, Err: err}
		}
		emitted++
	}
}

// Stepper releases the segments of one replay to a consumer that pulls them,
// on the same schedule as Replay: t0 is the Start call and segment i is due at
// t0 + endMs_i / Speed. A consumer that pulls late gets the segment
// immediately, so processing time shows up as lateness (EmittedAt after DueAt)
// and never as a dropped or reordered segment. A Stepper is not safe for
// concurrent use.
type Stepper struct {
	sim      *Simulator
	segments []contracts.MediaSegment
	offsets  []time.Duration
	t0       time.Time
	next     int
}

// Start validates segments and starts their schedule now.
func (s *Simulator) Start(segments []contracts.MediaSegment) (*Stepper, error) {
	if err := checkOrder(segments); err != nil {
		return nil, err
	}
	offsets, err := s.offsets(segments)
	if err != nil {
		return nil, err
	}
	return &Stepper{sim: s, segments: segments, offsets: offsets, t0: s.clock.Now()}, nil
}

// Next waits until the next segment is due and returns it. It returns io.EOF
// after the last segment and a StageCancelled error when ctx is done, in which
// case the segment is not released.
func (st *Stepper) Next(ctx context.Context) (Emission, error) {
	if st.next >= len(st.segments) {
		return Emission{}, io.EOF
	}
	i := st.next
	segment := st.segments[i]
	if err := ctx.Err(); err != nil {
		return Emission{}, &Error{Stage: StageCancelled, SegmentID: segment.SegmentID, Err: err}
	}
	due := st.t0.Add(st.offsets[i])
	if wait := due.Sub(st.sim.clock.Now()); wait > 0 {
		if err := st.sim.clock.Sleep(ctx, wait); err != nil {
			return Emission{}, &Error{Stage: StageCancelled, SegmentID: segment.SegmentID, Err: err}
		}
	}
	if err := ctx.Err(); err != nil {
		return Emission{}, &Error{Stage: StageCancelled, SegmentID: segment.SegmentID, Err: err}
	}
	st.next++
	return Emission{Sequence: i, Segment: segment, DueAt: due, EmittedAt: st.sim.clock.Now()}, nil
}

// StartedAt is the schedule origin t0.
func (st *Stepper) StartedAt() time.Time { return st.t0 }

func (s *Simulator) offsets(segments []contracts.MediaSegment) ([]time.Duration, error) {
	offsets := make([]time.Duration, len(segments))
	if s.pacing.Instant {
		return offsets, nil
	}
	for i, segment := range segments {
		ns := float64(segment.Window.EndMs) * float64(time.Millisecond) / s.pacing.Speed
		if ns >= math.MaxInt64 {
			return nil, &Error{Stage: StagePacing, SegmentID: segment.SegmentID, Err: fmt.Errorf(
				"%w: endMs %d at speed %v overflows the schedule", ErrInvalidPacing, segment.Window.EndMs, s.pacing.Speed)}
		}
		offsets[i] = time.Duration(ns)
	}
	return offsets, nil
}

func checkOrder(segments []contracts.MediaSegment) error {
	for i, segment := range segments {
		if err := contracts.Validate(segment); err != nil {
			return &Error{Stage: StageSegment, SegmentID: segment.SegmentID, Err: err}
		}
		if i == 0 {
			continue
		}
		prev := segments[i-1].Window
		if segment.Window.StartMs < prev.EndMs {
			return &Error{Stage: StageSegment, SegmentID: segment.SegmentID, Err: fmt.Errorf(
				"window [%d, %d] starts before the previous segment ends at %d; segments must be ordered and non-overlapping",
				segment.Window.StartMs, segment.Window.EndMs, prev.EndMs)}
		}
	}
	return nil
}
