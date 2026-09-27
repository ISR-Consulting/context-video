package media_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ISR-Consulting/context-video/internal/media"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

var specsRoot = filepath.Join("..", "..", "specs")

func newValidator(t *testing.T) *contracts.Validator {
	t.Helper()
	v, err := contracts.NewValidator(os.DirFS(specsRoot))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func source(contentType contracts.ContentType, durationMs int64) media.Source {
	return media.Source{
		Content:    contracts.ContentRef{ContentID: "content-1", ContentType: contentType},
		URI:        "media/content-1.fixture",
		DurationMs: durationMs,
	}
}

func TestSegmentWindows(t *testing.T) {
	validator := newValidator(t)
	cases := []struct {
		name       string
		durationMs int64
		window     time.Duration
		want       [][2]int64
	}{
		{"exact multiple", 10000, 5 * time.Second, [][2]int64{{0, 5000}, {5000, 10000}}},
		{"partial last window", 8000, 5 * time.Second, [][2]int64{{0, 5000}, {5000, 8000}}},
		{"one millisecond tail", 10001, 5 * time.Second, [][2]int64{{0, 5000}, {5000, 10000}, {10000, 10001}}},
		{"window longer than media", 3000, 10 * time.Second, [][2]int64{{0, 3000}}},
		{"window equals duration", 5000, 5 * time.Second, [][2]int64{{0, 5000}}},
		{"one millisecond windows", 3, time.Millisecond, [][2]int64{{0, 1}, {1, 2}, {2, 3}}},
		{"fixture live 2s", 10000, 2 * time.Second, [][2]int64{{0, 2000}, {2000, 4000}, {4000, 6000}, {6000, 8000}, {8000, 10000}}},
		{"fixture live 10s", 10000, 10 * time.Second, [][2]int64{{0, 10000}}},
		{"fixture vod 2s", 8000, 2 * time.Second, [][2]int64{{0, 2000}, {2000, 4000}, {4000, 6000}, {6000, 8000}}},
		{"fixture vod 10s", 8000, 10 * time.Second, [][2]int64{{0, 8000}}},
	}
	for _, contentType := range []contracts.ContentType{contracts.ContentTypeLive, contracts.ContentTypeVOD} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("%s/%s", contentType, tc.name), func(t *testing.T) {
				src := source(contentType, tc.durationMs)
				segments, err := media.Segment(src, tc.window)
				if err != nil {
					t.Fatal(err)
				}
				windowMs := tc.window.Milliseconds()
				if want := (tc.durationMs + windowMs - 1) / windowMs; int64(len(segments)) != want {
					t.Fatalf("got %d segments, want ceil = %d", len(segments), want)
				}
				if len(segments) != len(tc.want) {
					t.Fatalf("got %d segments, want %d", len(segments), len(tc.want))
				}
				ids := map[string]bool{}
				var prevEnd int64
				for i, segment := range segments {
					w := segment.Window
					if w.StartMs != tc.want[i][0] || w.EndMs != tc.want[i][1] {
						t.Fatalf("segment %d window = [%d, %d], want %v", i, w.StartMs, w.EndMs, tc.want[i])
					}
					if w.StartMs != prevEnd {
						t.Fatalf("segment %d starts at %d, previous ended at %d", i, w.StartMs, prevEnd)
					}
					prevEnd = w.EndMs
					wantID := fmt.Sprintf("content-1:%d-%d", w.StartMs, w.EndMs)
					if segment.SegmentID != wantID || media.SegmentID("content-1", w) != wantID {
						t.Fatalf("segment id %q, want %q", segment.SegmentID, wantID)
					}
					if ids[segment.SegmentID] {
						t.Fatalf("duplicate segment id %q", segment.SegmentID)
					}
					ids[segment.SegmentID] = true
					if segment.Content != src.Content {
						t.Fatalf("content %+v, want %+v", segment.Content, src.Content)
					}
					if segment.SourceURI == nil || *segment.SourceURI != src.URI {
						t.Fatalf("sourceUri %v, want %q", segment.SourceURI, src.URI)
					}
					if segment.Metadata != nil {
						t.Fatalf("metadata %v, want none", segment.Metadata)
					}
					data, err := json.Marshal(segment)
					if err != nil {
						t.Fatal(err)
					}
					if err := validator.ValidateJSON(contracts.KindMediaSegment, data); err != nil {
						t.Fatalf("schema: %v", err)
					}
					if err := contracts.Validate(segment); err != nil {
						t.Fatalf("domain: %v", err)
					}
				}
				if prevEnd != tc.durationMs {
					t.Fatalf("segments end at %d, want %d", prevEnd, tc.durationMs)
				}

				again, err := media.Segment(src, tc.window)
				if err != nil {
					t.Fatal(err)
				}
				first, _ := json.Marshal(segments)
				second, _ := json.Marshal(again)
				if !bytes.Equal(first, second) {
					t.Fatalf("non-deterministic output:\n%s\n%s", first, second)
				}
			})
		}
	}
}

func TestSegmentSourceURIsAreIndependent(t *testing.T) {
	segments, err := media.Segment(source(contracts.ContentTypeVOD, 10000), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	*segments[0].SourceURI = "mutated"
	if *segments[1].SourceURI != "media/content-1.fixture" {
		t.Fatalf("segments share sourceUri storage: %q", *segments[1].SourceURI)
	}
}

func TestSegmentOmitsEmptySourceURI(t *testing.T) {
	src := source(contracts.ContentTypeVOD, 1000)
	src.URI = ""
	segments, err := media.Segment(src, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if segments[0].SourceURI != nil {
		t.Fatalf("sourceUri = %q, want omitted", *segments[0].SourceURI)
	}
}

func TestSegmentInvalidInput(t *testing.T) {
	valid := source(contracts.ContentTypeLive, 10000)
	cases := []struct {
		name     string
		mutate   func(*media.Source)
		window   time.Duration
		sentinel error
		stage    media.Stage
	}{
		{"blank content id", func(s *media.Source) { s.Content.ContentID = "  " }, time.Second, media.ErrInvalidSource, media.StageSource},
		{"bad content type", func(s *media.Source) { s.Content.ContentType = "CLIP" }, time.Second, media.ErrInvalidSource, media.StageSource},
		{"empty content type", func(s *media.Source) { s.Content.ContentType = "" }, time.Second, media.ErrInvalidSource, media.StageSource},
		{"zero duration", func(s *media.Source) { s.DurationMs = 0 }, time.Second, media.ErrInvalidSource, media.StageSource},
		{"negative duration", func(s *media.Source) { s.DurationMs = -1 }, time.Second, media.ErrInvalidSource, media.StageSource},
		{"zero window", nil, 0, media.ErrInvalidWindow, media.StageWindow},
		{"negative window", nil, -time.Second, media.ErrInvalidWindow, media.StageWindow},
		{"sub-millisecond window", nil, 1500 * time.Microsecond, media.ErrInvalidWindow, media.StageWindow},
		{"too many segments", func(s *media.Source) { s.DurationMs = media.MaxSegments + 1 }, time.Millisecond, media.ErrInvalidWindow, media.StageWindow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := valid
			if tc.mutate != nil {
				tc.mutate(&src)
			}
			segments, err := media.Segment(src, tc.window)
			if err == nil {
				t.Fatalf("expected error, got %d segments", len(segments))
			}
			if segments != nil {
				t.Fatalf("segments returned on error: %v", segments)
			}
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("error %v is not %v", err, tc.sentinel)
			}
			var mediaErr *media.Error
			if !errors.As(err, &mediaErr) || mediaErr.Stage != tc.stage {
				t.Fatalf("error %v: want stage %q", err, tc.stage)
			}
		})
	}
}

func TestSegmentAtLimit(t *testing.T) {
	segments, err := media.Segment(source(contracts.ContentTypeVOD, media.MaxSegments), time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != media.MaxSegments {
		t.Fatalf("got %d segments, want %d", len(segments), media.MaxSegments)
	}
}

func TestSegmentJoinsAllInputErrors(t *testing.T) {
	_, err := media.Segment(media.Source{}, 0)
	if !errors.Is(err, media.ErrInvalidSource) || !errors.Is(err, media.ErrInvalidWindow) {
		t.Fatalf("expected source and window errors: %v", err)
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("expected joined error, got %T", err)
	}
	if n := len(joined.Unwrap()); n != 4 {
		t.Fatalf("got %d errors, want 4 (content id, content type, duration, window): %v", n, err)
	}
}
