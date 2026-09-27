package media

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// MaxSegments bounds the number of segments one Segment call may produce.
const MaxSegments = 1 << 20

// SegmentID returns the deterministic identifier of the segment of contentID
// covering window: "<contentId>:<startMs>-<endMs>".
func SegmentID(contentID string, window contracts.TimeWindow) string {
	return fmt.Sprintf("%s:%d-%d", contentID, window.StartMs, window.EndMs)
}

// Segment cuts src into back-to-back windows of the given size covering
// [0, src.DurationMs]. The last window is truncated at the duration, so a
// window longer than the media yields exactly one segment. Content and URI are
// copied unchanged and no metadata is added, so the result is deterministic.
// Every segment passes contracts.Validate. Input problems are joined into one
// error.
func Segment(src Source, window time.Duration) ([]contracts.MediaSegment, error) {
	var errs []error
	sourceErr := func(format string, args ...any) {
		errs = append(errs, &Error{Stage: StageSource, Err: fmt.Errorf("%w: "+format, append([]any{ErrInvalidSource}, args...)...)})
	}
	if strings.TrimSpace(src.Content.ContentID) == "" {
		sourceErr("contentId must not be blank")
	}
	switch src.Content.ContentType {
	case contracts.ContentTypeLive, contracts.ContentTypeVOD:
	default:
		sourceErr("contentType %q must be %s or %s", src.Content.ContentType, contracts.ContentTypeLive, contracts.ContentTypeVOD)
	}
	if src.DurationMs < 1 {
		sourceErr("durationMs %d must be >= 1", src.DurationMs)
	}
	if window <= 0 || window%time.Millisecond != 0 {
		errs = append(errs, &Error{Stage: StageWindow, Err: fmt.Errorf(
			"%w: %s must be positive and a whole number of milliseconds", ErrInvalidWindow, window)})
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	windowMs := window.Milliseconds()
	count := src.DurationMs / windowMs
	if src.DurationMs%windowMs != 0 {
		count++
	}
	if count > MaxSegments {
		return nil, &Error{Stage: StageWindow, Err: fmt.Errorf(
			"%w: %s over %d ms yields %d segments, above the limit of %d", ErrInvalidWindow, window, src.DurationMs, count, MaxSegments)}
	}

	segments := make([]contracts.MediaSegment, 0, count)
	for start := int64(0); start < src.DurationMs; {
		end := start + min(windowMs, src.DurationMs-start)
		tw := contracts.TimeWindow{StartMs: start, EndMs: end}
		segment := contracts.MediaSegment{
			SegmentID: SegmentID(src.Content.ContentID, tw),
			Content:   src.Content,
			Window:    tw,
		}
		if src.URI != "" {
			uri := src.URI
			segment.SourceURI = &uri
		}
		if err := contracts.Validate(segment); err != nil {
			return nil, &Error{Stage: StageSegment, SegmentID: segment.SegmentID, Err: err}
		}
		segments = append(segments, segment)
		start = end
	}
	return segments, nil
}
