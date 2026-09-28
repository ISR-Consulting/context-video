package vision

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// MaxUniformFrames bounds the frames per window of the uniform policy.
const MaxUniformFrames = 16

// ErrUnsupportedSampling reports a vision.sampling value with no policy.
var ErrUnsupportedSampling = errors.New("unsupported vision sampling policy")

// Sampler chooses the frame timestamps analyzed inside a window.
type Sampler interface {
	FrameTimes(window contracts.TimeWindow) ([]int64, error)
}

// Uniform samples Frames timestamps at the centres of Frames equal
// sub-intervals of [StartMs, EndMs). Every timestamp satisfies
// StartMs <= t < EndMs, so adjacent windows never share a frame.
type Uniform struct {
	Frames int
}

// String returns the vision.sampling value of u, e.g. "uniform:2".
func (u Uniform) String() string { return "uniform:" + strconv.Itoa(u.Frames) }

// FrameTimes returns the ascending, de-duplicated timestamps for window.
// Windows shorter than 2·Frames ms may yield fewer timestamps, never none.
func (u Uniform) FrameTimes(window contracts.TimeWindow) ([]int64, error) {
	if u.Frames < 1 || u.Frames > MaxUniformFrames {
		return nil, fmt.Errorf("uniform sampling needs 1..%d frames, got %d", MaxUniformFrames, u.Frames)
	}
	if window.StartMs < 0 || window.EndMs <= window.StartMs {
		return nil, fmt.Errorf("invalid window [%d, %d]", window.StartMs, window.EndMs)
	}
	duration := window.EndMs - window.StartMs
	n := int64(u.Frames)
	times := make([]int64, 0, u.Frames)
	for i := range n {
		t := window.StartMs + (2*i+1)*duration/(2*n)
		if len(times) == 0 || times[len(times)-1] != t {
			times = append(times, t)
		}
	}
	return times, nil
}

// ParseSampling parses a vision.sampling configuration value. The only policy
// is "uniform:N" with N in 1..MaxUniformFrames; scene-change selection is not
// implemented. Other values, including "TBD", wrap ErrUnsupportedSampling.
func ParseSampling(raw string) (Sampler, error) {
	name, arg, ok := strings.Cut(strings.TrimSpace(raw), ":")
	if !ok || name != "uniform" {
		return nil, fmt.Errorf("%w %q; supported: uniform:N (N = 1..%d)", ErrUnsupportedSampling, raw, MaxUniformFrames)
	}
	n, err := strconv.Atoi(arg)
	if err != nil || n < 1 || n > MaxUniformFrames {
		return nil, fmt.Errorf("%w %q: N must be an integer in 1..%d", ErrUnsupportedSampling, raw, MaxUniformFrames)
	}
	return Uniform{Frames: n}, nil
}
