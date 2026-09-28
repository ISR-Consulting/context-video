package vision

import (
	"context"
	"errors"

	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// SourceKind describes how the frames of a request are made available. The
// values mirror the golden dataset media kinds without importing them.
type SourceKind string

const (
	SourceLocal            SourceKind = "LOCAL"
	SourceFixture          SourceKind = "FIXTURE"
	SourceControlledSource SourceKind = "CONTROLLED_SOURCE"
)

// Source locates the media whose frames are analyzed.
type Source struct {
	Kind SourceKind
	// URI is the dataset media URI. It is descriptive and never fetched.
	URI string
	// Path is a host file path. It is set only for LOCAL and FIXTURE media.
	Path string
}

// Request asks for the analysis of the frames at FrameTimesMs of Source. The
// timestamps are media time, ascending, and inside Segment.Window.
type Request struct {
	Segment      contracts.MediaSegment
	Source       Source
	FrameTimesMs []int64
}

// Detection is one provider-neutral structured observation on a frame.
type Detection struct {
	Type  contracts.VisualDetectionType
	Value string
	// Confidence is nil when the provider supplied none. NewObservation
	// rejects nil because the contract requires item-level confidence.
	Confidence *float64
}

// Frame holds the detections of one analyzed frame.
type Frame struct {
	TimestampMs int64
	// Description is empty when the provider gives none.
	Description string
	Detections  []Detection
}

// Analysis is the provider-neutral result for one request: one Frame per
// requested timestamp, in request order.
type Analysis struct {
	Frames []Frame
	// Provider is the registered provider name. It is required.
	Provider string
	// Model is a descriptive model label, empty when unknown. It must not be a
	// host path.
	Model string
}

// Analyzer analyzes the sampled frames of one media window. Implementations
// must honor ctx cancellation and must not return provider-native types.
type Analyzer interface {
	Analyze(ctx context.Context, req Request) (Analysis, error)
}

// AnalyzerFunc adapts a function to Analyzer.
type AnalyzerFunc func(ctx context.Context, req Request) (Analysis, error)

// Analyze calls f.
func (f AnalyzerFunc) Analyze(ctx context.Context, req Request) (Analysis, error) {
	return f(ctx, req)
}

// ErrUnsupportedSource reports a Source an analyzer cannot read, such as
// CONTROLLED_SOURCE media for an adapter that only reads local files.
var ErrUnsupportedSource = errors.New("unsupported vision source")

// DetectionTypes returns the closed set of contract detection types.
func DetectionTypes() []contracts.VisualDetectionType {
	return []contracts.VisualDetectionType{
		contracts.VisualDetectionObject,
		contracts.VisualDetectionEntity,
		contracts.VisualDetectionTopic,
		contracts.VisualDetectionBrand,
		contracts.VisualDetectionText,
		contracts.VisualDetectionScene,
		contracts.VisualDetectionAction,
	}
}
