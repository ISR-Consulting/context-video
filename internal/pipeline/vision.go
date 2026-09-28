package pipeline

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ISR-Consulting/context-video/internal/evaluation/dataset"
	"github.com/ISR-Consulting/context-video/internal/evaluation/harness"
	"github.com/ISR-Consulting/context-video/internal/media"
	"github.com/ISR-Consulting/context-video/internal/vision"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// VisionSourceResolver locates the media of a test case for an analyzer.
type VisionSourceResolver func(media dataset.MediaReference) (vision.Source, error)

// VisionDirResolver resolves LOCAL and FIXTURE media URIs against datasetRoot,
// the directory the M02 loader verified them in. CONTROLLED_SOURCE media gets
// no host path.
func VisionDirResolver(datasetRoot string) VisionSourceResolver {
	return func(ref dataset.MediaReference) (vision.Source, error) {
		src := vision.Source{Kind: vision.SourceKind(ref.Kind), URI: ref.URI}
		switch ref.Kind {
		case dataset.MediaKindLocal, dataset.MediaKindFixture:
			src.Path = filepath.Join(datasetRoot, filepath.FromSlash(ref.URI))
		case dataset.MediaKindControlledSource:
		default:
			return vision.Source{}, fmt.Errorf("unknown media kind %q", ref.Kind)
		}
		return src, nil
	}
}

// VisionOption customizes a Vision pipeline.
type VisionOption func(*Vision)

// WithVisionPipelineVersion overrides DefaultPipelineVersion.
func WithVisionPipelineVersion(version string) VisionOption {
	return func(v *Vision) { v.pipelineVersion = version }
}

// Vision is the vision-only harness pipeline.
type Vision struct {
	provider        string
	analyzer        vision.Analyzer
	resolve         VisionSourceResolver
	pipelineVersion string
}

// NewVision returns a Vision pipeline for the analyzer registered as provider.
// Process rejects configurations naming another vision provider.
func NewVision(provider string, analyzer vision.Analyzer, resolve VisionSourceResolver, opts ...VisionOption) (*Vision, error) {
	if strings.TrimSpace(provider) == "" {
		return nil, errors.New("pipeline: blank vision provider")
	}
	if analyzer == nil {
		return nil, errors.New("pipeline: nil analyzer")
	}
	if resolve == nil {
		return nil, errors.New("pipeline: nil source resolver")
	}
	v := &Vision{provider: provider, analyzer: analyzer, resolve: resolve, pipelineVersion: DefaultPipelineVersion}
	for _, opt := range opts {
		opt(v)
	}
	if strings.TrimSpace(v.pipelineVersion) == "" {
		return nil, errors.New("pipeline: blank pipeline version")
	}
	return v, nil
}

// Process samples frames in every window of the test case according to
// vision.sampling, analyzes them sequentially and in media order, and returns
// one VisualObservation per window.
func (v *Vision) Process(ctx context.Context, in harness.PipelineInput) (harness.PipelineOutput, error) {
	cfg := in.Config
	switch {
	case !cfg.Vision.Enabled:
		return harness.PipelineOutput{}, fmt.Errorf("vision pipeline requires vision.enabled: true in experiment %s", cfg.ExperimentID)
	case cfg.Audio.Enabled:
		return harness.PipelineOutput{}, fmt.Errorf(
			"vision pipeline does not run audio; experiment %s enables audio (use a vision-only configuration)", cfg.ExperimentID)
	case cfg.Vision.Provider != v.provider:
		return harness.PipelineOutput{}, fmt.Errorf(
			"experiment %s selects vision.provider %q but the pipeline was built for %q", cfg.ExperimentID, cfg.Vision.Provider, v.provider)
	}
	sampler, err := vision.ParseSampling(cfg.Vision.Sampling)
	if err != nil {
		return harness.PipelineOutput{}, fmt.Errorf("experiment %s vision.sampling: %w", cfg.ExperimentID, err)
	}

	testCase := in.TestCase.TestCase
	source, err := v.resolve(testCase.Media)
	if err != nil {
		return harness.PipelineOutput{}, fmt.Errorf("resolve media %q: %w", testCase.Media.URI, err)
	}
	segments, err := media.Segment(media.Source{
		Content:    testCase.Content,
		URI:        testCase.Media.URI,
		DurationMs: testCase.Media.DurationMs,
	}, cfg.WindowSize)
	if err != nil {
		return harness.PipelineOutput{}, err
	}

	observations := make([]contracts.VisualObservation, 0, len(segments))
	for _, segment := range segments {
		if err := ctx.Err(); err != nil {
			return harness.PipelineOutput{}, err
		}
		times, err := sampler.FrameTimes(segment.Window)
		if err != nil {
			return harness.PipelineOutput{}, fmt.Errorf("sample segment %s: %w", segment.SegmentID, err)
		}
		analysis, err := v.analyzer.Analyze(ctx, vision.Request{Segment: segment, Source: source, FrameTimesMs: times})
		if err != nil {
			return harness.PipelineOutput{}, fmt.Errorf("analyze segment %s: %w", segment.SegmentID, err)
		}
		observation, err := vision.NewObservation(segment, times, analysis, v.pipelineVersion)
		if err != nil {
			return harness.PipelineOutput{}, err
		}
		observations = append(observations, observation)
	}
	return harness.PipelineOutput{VisualObservations: observations}, nil
}

var _ harness.Pipeline = (*Vision)(nil)
