package pipeline

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ISR-Consulting/context-video/internal/audio"
	"github.com/ISR-Consulting/context-video/internal/evaluation/dataset"
	"github.com/ISR-Consulting/context-video/internal/evaluation/harness"
	"github.com/ISR-Consulting/context-video/internal/media"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// DefaultPipelineVersion is the provenance pipelineVersion of emitted
// observations unless overridden.
const DefaultPipelineVersion = "poc-v1"

// SourceResolver locates the media of a test case for a transcriber.
type SourceResolver func(media dataset.MediaReference) (audio.Source, error)

// DirResolver resolves LOCAL and FIXTURE media URIs against datasetRoot, the
// directory the M02 loader verified them in. CONTROLLED_SOURCE media gets no
// host path.
func DirResolver(datasetRoot string) SourceResolver {
	return func(ref dataset.MediaReference) (audio.Source, error) {
		src := audio.Source{Kind: audio.SourceKind(ref.Kind), URI: ref.URI}
		switch ref.Kind {
		case dataset.MediaKindLocal, dataset.MediaKindFixture:
			src.Path = filepath.Join(datasetRoot, filepath.FromSlash(ref.URI))
		case dataset.MediaKindControlledSource:
		default:
			return audio.Source{}, fmt.Errorf("unknown media kind %q", ref.Kind)
		}
		return src, nil
	}
}

// AudioOption customizes an Audio pipeline.
type AudioOption func(*Audio)

// WithPipelineVersion overrides DefaultPipelineVersion.
func WithPipelineVersion(version string) AudioOption {
	return func(a *Audio) { a.pipelineVersion = version }
}

// Audio is the audio-only harness pipeline.
type Audio struct {
	provider        string
	transcriber     audio.Transcriber
	resolve         SourceResolver
	pipelineVersion string
}

// NewAudio returns an Audio pipeline for the transcriber registered as
// provider. Process rejects configurations naming another audio provider.
func NewAudio(provider string, transcriber audio.Transcriber, resolve SourceResolver, opts ...AudioOption) (*Audio, error) {
	if strings.TrimSpace(provider) == "" {
		return nil, errors.New("pipeline: blank audio provider")
	}
	if transcriber == nil {
		return nil, errors.New("pipeline: nil transcriber")
	}
	if resolve == nil {
		return nil, errors.New("pipeline: nil source resolver")
	}
	a := &Audio{provider: provider, transcriber: transcriber, resolve: resolve, pipelineVersion: DefaultPipelineVersion}
	for _, opt := range opts {
		opt(a)
	}
	if strings.TrimSpace(a.pipelineVersion) == "" {
		return nil, errors.New("pipeline: blank pipeline version")
	}
	return a, nil
}

// Process transcribes every window of the test case, sequentially and in media
// order, and returns one AudioObservation per window.
func (a *Audio) Process(ctx context.Context, in harness.PipelineInput) (harness.PipelineOutput, error) {
	cfg := in.Config
	switch {
	case !cfg.Audio.Enabled:
		return harness.PipelineOutput{}, fmt.Errorf("audio pipeline requires audio.enabled: true in experiment %s", cfg.ExperimentID)
	case cfg.Vision.Enabled:
		return harness.PipelineOutput{}, fmt.Errorf(
			"audio pipeline does not run vision; experiment %s enables vision (use an audio-only configuration)", cfg.ExperimentID)
	case cfg.Audio.Provider != a.provider:
		return harness.PipelineOutput{}, fmt.Errorf(
			"experiment %s selects audio.provider %q but the pipeline was built for %q", cfg.ExperimentID, cfg.Audio.Provider, a.provider)
	}

	testCase := in.TestCase.TestCase
	source, err := a.resolve(testCase.Media)
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

	observations := make([]contracts.AudioObservation, 0, len(segments))
	for _, segment := range segments {
		if err := ctx.Err(); err != nil {
			return harness.PipelineOutput{}, err
		}
		transcription, err := a.transcriber.Transcribe(ctx, audio.Request{Segment: segment, Source: source})
		if err != nil {
			return harness.PipelineOutput{}, fmt.Errorf("transcribe segment %s: %w", segment.SegmentID, err)
		}
		observation, err := audio.NewObservation(segment, transcription, a.pipelineVersion)
		if err != nil {
			return harness.PipelineOutput{}, err
		}
		observations = append(observations, observation)
	}
	return harness.PipelineOutput{AudioObservations: observations}, nil
}

var _ harness.Pipeline = (*Audio)(nil)
