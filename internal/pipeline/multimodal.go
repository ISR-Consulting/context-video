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

// SegmentObserver is notified around the segments of a Multimodal run, in
// media order and on the calling goroutine. It is how a harness paces
// segments (BeforeSegment may block until a segment is due) and records
// per-segment outcomes without the pipeline knowing about either.
type SegmentObserver interface {
	// BeginCase is called once per test case, before any segment of it.
	BeginCase(ctx context.Context, testCaseID string, segments []contracts.MediaSegment) error
	// BeforeSegment is called before a segment's perception starts. An error
	// aborts the run.
	BeforeSegment(ctx context.Context, segment contracts.MediaSegment) error
	// AfterSegment is called once per processed segment with the events it
	// produced or with its error. Without WithRecordedSegmentErrors the run
	// then aborts with that error.
	AfterSegment(segment contracts.MediaSegment, events []contracts.ContextEventV1, err error)
}

// WithSegmentObserver notifies observer around every segment.
func WithSegmentObserver(observer SegmentObserver) MultimodalOption {
	return func(m *Multimodal) { m.observer = observer }
}

// WithRecordedSegmentErrors makes a failing segment contribute no
// ContextEvents instead of failing the run. The failure is passed to the
// SegmentObserver, which is then required. Observations the segment produced
// before failing are kept. Cancellation still aborts the run.
func WithRecordedSegmentErrors() MultimodalOption {
	return func(m *Multimodal) { m.recordErrors = true }
}

// Multimodal is the context reasoning harness pipeline. For every M04 segment
// it runs the enabled perception ports, then the context engine, and returns
// the observations together with the ContextEvents derived from them.
type Multimodal struct {
	cfg             MultimodalConfig
	pipelineVersion string
	observer        SegmentObserver
	recordErrors    bool
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
	if m.recordErrors && m.observer == nil {
		return nil, errors.New("pipeline: recorded segment errors need a segment observer")
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

	if m.observer != nil {
		if err := m.observer.BeginCase(ctx, testCase.TestCaseID, segments); err != nil {
			return harness.PipelineOutput{}, err
		}
	}
	var out harness.PipelineOutput
	for _, segment := range segments {
		if err := ctx.Err(); err != nil {
			return harness.PipelineOutput{}, err
		}
		if m.observer != nil {
			if err := m.observer.BeforeSegment(ctx, segment); err != nil {
				return harness.PipelineOutput{}, err
			}
		}
		events, err := m.processSegment(ctx, segment, audioSource, visionSource, sampler, &out)
		if err != nil {
			if m.observer != nil {
				m.observer.AfterSegment(segment, nil, err)
			}
			if !m.recordErrors || ctx.Err() != nil {
				return harness.PipelineOutput{}, err
			}
			continue
		}
		out.ContextEvents = append(out.ContextEvents, events...)
		if m.observer != nil {
			m.observer.AfterSegment(segment, events, nil)
		}
	}
	return out, nil
}

// processSegment runs perception, correlation and reasoning for one segment,
// appending its observations to out as soon as each exists.
func (m *Multimodal) processSegment(ctx context.Context, segment contracts.MediaSegment, audioSource audio.Source,
	visionSource vision.Source, sampler vision.Sampler, out *harness.PipelineOutput) ([]contracts.ContextEventV1, error) {
	input := contextcore.CorrelationInput{Segment: segment}
	if m.cfg.Transcriber != nil {
		transcription, err := m.cfg.Transcriber.Transcribe(ctx, audio.Request{Segment: segment, Source: audioSource})
		if err != nil {
			return nil, contextcore.SegmentError(contextcore.StageAudio, segment, m.cfg.AudioProvider, err)
		}
		observation, err := audio.NewObservation(segment, transcription, m.pipelineVersion)
		if err != nil {
			return nil, contextcore.SegmentError(contextcore.StageAudio, segment, m.cfg.AudioProvider, err)
		}
		input.Audio = []contracts.AudioObservation{observation}
		out.AudioObservations = append(out.AudioObservations, observation)
	}
	if m.cfg.Analyzer != nil {
		times, err := sampler.FrameTimes(segment.Window)
		if err != nil {
			return nil, contextcore.SegmentError(contextcore.StageVision, segment, m.cfg.VisionProvider, err)
		}
		analysis, err := m.cfg.Analyzer.Analyze(ctx, vision.Request{Segment: segment, Source: visionSource, FrameTimesMs: times})
		if err != nil {
			return nil, contextcore.SegmentError(contextcore.StageVision, segment, m.cfg.VisionProvider, err)
		}
		observation, err := vision.NewObservation(segment, times, analysis, m.pipelineVersion)
		if err != nil {
			return nil, contextcore.SegmentError(contextcore.StageVision, segment, m.cfg.VisionProvider, err)
		}
		input.Visual = []contracts.VisualObservation{observation}
		out.VisualObservations = append(out.VisualObservations, observation)
	}
	return m.cfg.Engine.Process(ctx, input)
}

func wiredWord(wired bool) string {
	if wired {
		return "enabled"
	}
	return "disabled"
}

var _ harness.Pipeline = (*Multimodal)(nil)
