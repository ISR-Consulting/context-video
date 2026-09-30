package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ISR-Consulting/context-video/internal/audio"
	contextcore "github.com/ISR-Consulting/context-video/internal/context"
	"github.com/ISR-Consulting/context-video/internal/evaluation/harness"
	"github.com/ISR-Consulting/context-video/internal/media"
	"github.com/ISR-Consulting/context-video/internal/vision"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// MultimodalConfig wires the perception ports and the context engine of a
// Multimodal pipeline. The audio fields are set only when the experiment
// enables audio, the vision fields only when it enables vision.
type MultimodalConfig struct {
	AudioProvider string
	Transcriber   audio.Transcriber
	AudioResolve  SourceResolver

	VisionProvider string
	Analyzer       vision.Analyzer
	VisionResolve  VisionSourceResolver

	Engine *contextcore.Engine
}

// MultimodalOption customizes a Multimodal pipeline.
type MultimodalOption func(*Multimodal)

// WithMultimodalPipelineVersion overrides DefaultPipelineVersion for the
// emitted observations. ContextEvent provenance follows the Engine.
func WithMultimodalPipelineVersion(version string) MultimodalOption {
	return func(m *Multimodal) { m.pipelineVersion = version }
}

// Multimodal is the context reasoning harness pipeline. For every M04 segment
// it runs the enabled perception ports, then the context engine, and returns
// the observations together with the ContextEvents derived from them.
type Multimodal struct {
	cfg             MultimodalConfig
	pipelineVersion string
}

// NewMultimodal returns a Multimodal pipeline. At least one modality must be
// wired and every wired modality needs a provider name, port and resolver.
func NewMultimodal(cfg MultimodalConfig, opts ...MultimodalOption) (*Multimodal, error) {
	var errs []error
	if cfg.Engine == nil {
		errs = append(errs, errors.New("nil context engine"))
	}
	audioWired := cfg.Transcriber != nil || cfg.AudioResolve != nil || cfg.AudioProvider != ""
	if audioWired && (strings.TrimSpace(cfg.AudioProvider) == "" || cfg.Transcriber == nil || cfg.AudioResolve == nil) {
		errs = append(errs, errors.New("audio needs a provider, a transcriber and a source resolver"))
	}
	visionWired := cfg.Analyzer != nil || cfg.VisionResolve != nil || cfg.VisionProvider != ""
	if visionWired && (strings.TrimSpace(cfg.VisionProvider) == "" || cfg.Analyzer == nil || cfg.VisionResolve == nil) {
		errs = append(errs, errors.New("vision needs a provider, an analyzer and a source resolver"))
	}
	if !audioWired && !visionWired {
		errs = append(errs, errors.New("no audio or vision modality wired"))
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("pipeline: %w", errors.Join(errs...))
	}
	m := &Multimodal{cfg: cfg, pipelineVersion: DefaultPipelineVersion}
	for _, opt := range opts {
		opt(m)
	}
	if strings.TrimSpace(m.pipelineVersion) == "" {
		return nil, errors.New("pipeline: blank pipeline version")
	}
	return m, nil
}

// Process runs, sequentially and in media order for every segment: audio
// perception, visual perception, correlation, reasoning and ContextEvent
// mapping. Segments without usable evidence contribute observations only.
func (m *Multimodal) Process(ctx context.Context, in harness.PipelineInput) (harness.PipelineOutput, error) {
	cfg := in.Config
	audioWired := m.cfg.Transcriber != nil
	visionWired := m.cfg.Analyzer != nil
	switch {
	case cfg.FusionProvider != m.cfg.Engine.Provider():
		return harness.PipelineOutput{}, fmt.Errorf(
			"experiment %s selects fusion.provider %q but the pipeline was built for %q", cfg.ExperimentID, cfg.FusionProvider, m.cfg.Engine.Provider())
	case cfg.Audio.Enabled != audioWired:
		return harness.PipelineOutput{}, fmt.Errorf(
			"experiment %s sets audio.enabled: %t but the pipeline was built with audio %s", cfg.ExperimentID, cfg.Audio.Enabled, wiredWord(audioWired))
	case cfg.Vision.Enabled != visionWired:
		return harness.PipelineOutput{}, fmt.Errorf(
			"experiment %s sets vision.enabled: %t but the pipeline was built with vision %s", cfg.ExperimentID, cfg.Vision.Enabled, wiredWord(visionWired))
	case audioWired && cfg.Audio.Provider != m.cfg.AudioProvider:
		return harness.PipelineOutput{}, fmt.Errorf(
			"experiment %s selects audio.provider %q but the pipeline was built for %q", cfg.ExperimentID, cfg.Audio.Provider, m.cfg.AudioProvider)
	case visionWired && cfg.Vision.Provider != m.cfg.VisionProvider:
		return harness.PipelineOutput{}, fmt.Errorf(
			"experiment %s selects vision.provider %q but the pipeline was built for %q", cfg.ExperimentID, cfg.Vision.Provider, m.cfg.VisionProvider)
	}
	var sampler vision.Sampler
	if visionWired {
		s, err := vision.ParseSampling(cfg.Vision.Sampling)
		if err != nil {
			return harness.PipelineOutput{}, fmt.Errorf("experiment %s vision.sampling: %w", cfg.ExperimentID, err)
		}
		sampler = s
	}

	testCase := in.TestCase.TestCase
	var audioSource audio.Source
	var visionSource vision.Source
	if audioWired {
		src, err := m.cfg.AudioResolve(testCase.Media)
		if err != nil {
			return harness.PipelineOutput{}, fmt.Errorf("resolve media %q: %w", testCase.Media.URI, err)
		}
		audioSource = src
	}
	if visionWired {
		src, err := m.cfg.VisionResolve(testCase.Media)
		if err != nil {
			return harness.PipelineOutput{}, fmt.Errorf("resolve media %q: %w", testCase.Media.URI, err)
		}
		visionSource = src
	}
	segments, err := media.Segment(media.Source{
		Content:    testCase.Content,
		URI:        testCase.Media.URI,
		DurationMs: testCase.Media.DurationMs,
	}, cfg.WindowSize)
	if err != nil {
		return harness.PipelineOutput{}, err
	}

	var out harness.PipelineOutput
	for _, segment := range segments {
		if err := ctx.Err(); err != nil {
			return harness.PipelineOutput{}, err
		}
		input := contextcore.CorrelationInput{Segment: segment}
		if audioWired {
			transcription, err := m.cfg.Transcriber.Transcribe(ctx, audio.Request{Segment: segment, Source: audioSource})
			if err != nil {
				return harness.PipelineOutput{}, contextcore.SegmentError(contextcore.StageAudio, segment, m.cfg.AudioProvider, err)
			}
			observation, err := audio.NewObservation(segment, transcription, m.pipelineVersion)
			if err != nil {
				return harness.PipelineOutput{}, contextcore.SegmentError(contextcore.StageAudio, segment, m.cfg.AudioProvider, err)
			}
			input.Audio = []contracts.AudioObservation{observation}
			out.AudioObservations = append(out.AudioObservations, observation)
		}
		if visionWired {
			times, err := sampler.FrameTimes(segment.Window)
			if err != nil {
				return harness.PipelineOutput{}, contextcore.SegmentError(contextcore.StageVision, segment, m.cfg.VisionProvider, err)
			}
			analysis, err := m.cfg.Analyzer.Analyze(ctx, vision.Request{Segment: segment, Source: visionSource, FrameTimesMs: times})
			if err != nil {
				return harness.PipelineOutput{}, contextcore.SegmentError(contextcore.StageVision, segment, m.cfg.VisionProvider, err)
			}
			observation, err := vision.NewObservation(segment, times, analysis, m.pipelineVersion)
			if err != nil {
				return harness.PipelineOutput{}, contextcore.SegmentError(contextcore.StageVision, segment, m.cfg.VisionProvider, err)
			}
			input.Visual = []contracts.VisualObservation{observation}
			out.VisualObservations = append(out.VisualObservations, observation)
		}
		events, err := m.cfg.Engine.Process(ctx, input)
		if err != nil {
			return harness.PipelineOutput{}, err
		}
		out.ContextEvents = append(out.ContextEvents, events...)
	}
	return out, nil
}

func wiredWord(wired bool) string {
	if wired {
		return "enabled"
	}
	return "disabled"
}

var _ harness.Pipeline = (*Multimodal)(nil)
