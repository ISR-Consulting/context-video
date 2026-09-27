package audio

import (
	"context"
	"errors"

	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// SourceKind describes how the audio bytes of a request are made available.
// The values mirror the golden dataset media kinds without importing them.
type SourceKind string

const (
	SourceLocal            SourceKind = "LOCAL"
	SourceFixture          SourceKind = "FIXTURE"
	SourceControlledSource SourceKind = "CONTROLLED_SOURCE"
)

// Source locates the media whose audio is transcribed.
type Source struct {
	Kind SourceKind
	// URI is the dataset media URI. It is descriptive and never fetched.
	URI string
	// Path is a host file path. It is set only for LOCAL and FIXTURE media.
	Path string
}

// Request asks for the transcription of exactly Segment.Window of Source, in
// media time.
type Request struct {
	Segment contracts.MediaSegment
	Source  Source
}

// Transcription is the provider-neutral result for one window.
type Transcription struct {
	// Text may be empty when the window contains no recognized speech.
	Text string
	// Language is empty when the provider reports none.
	Language string
	// Confidence is nil unless the provider itself supplies a 0..1 value.
	Confidence *float64
	// Provider is the registered provider name. It is required.
	Provider string
	// Model is a descriptive model label, empty when unknown. It must not be a
	// host path.
	Model string
}

// Transcriber transcribes the audio of one media window. Implementations must
// honor ctx cancellation and must not return provider-native types.
type Transcriber interface {
	Transcribe(ctx context.Context, req Request) (Transcription, error)
}

// TranscriberFunc adapts a function to Transcriber.
type TranscriberFunc func(ctx context.Context, req Request) (Transcription, error)

// Transcribe calls f.
func (f TranscriberFunc) Transcribe(ctx context.Context, req Request) (Transcription, error) {
	return f(ctx, req)
}

// ErrUnsupportedSource reports a Source a transcriber cannot read, such as
// CONTROLLED_SOURCE media for an adapter that only reads local files.
var ErrUnsupportedSource = errors.New("unsupported audio source")
