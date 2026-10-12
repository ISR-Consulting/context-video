package openai

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ISR-Consulting/context-video/internal/vision"
)

// ProviderName is the registry name and provenance provider of this adapter.
const ProviderName = "openai-vision"

// Step identifies which part of an analysis failed.
type Step string

const (
	StepSource    Step = "source"
	StepWorkspace Step = "workspace"
	StepExtract   Step = "extract"
	StepAnalyze   Step = "analyze"
	StepParse     Step = "parse"
)

// Error is a step-qualified adapter failure.
type Error struct {
	Step      Step
	SegmentID string
	FrameMs   *int64
	Stderr    string
	Output    string
	Err       error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	parts := []string{ProviderName + " " + string(e.Step)}
	if e.SegmentID != "" {
		parts = append(parts, "segment "+e.SegmentID)
	}
	if e.FrameMs != nil {
		parts = append(parts, fmt.Sprintf("frame %dms", *e.FrameMs))
	}
	if e.Err != nil {
		parts = append(parts, e.Err.Error())
	}
	msg := strings.Join(parts, ": ")
	if e.Output != "" {
		msg += " (output: " + e.Output + ")"
	}
	if e.Stderr != "" {
		msg += " (stderr: " + e.Stderr + ")"
	}
	return msg
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Adapter analyzes sampled frames with ffmpeg and OpenAI vision.
type Adapter struct {
	cfg    Config
	client *http.Client
	runner CommandRunner
	tmpDir string
}

// AdapterOption customizes an Adapter.
type AdapterOption func(*Adapter)

// WithHTTPClient replaces the HTTP client used for OpenAI requests.
func WithHTTPClient(c *http.Client) AdapterOption {
	return func(a *Adapter) {
		if c != nil {
			a.client = c
		}
	}
}

// WithRunner replaces the os/exec command runner (ffmpeg).
func WithRunner(r CommandRunner) AdapterOption {
	return func(a *Adapter) {
		if r != nil {
			a.runner = r
		}
	}
}

// WithTempDir sets the parent directory for per-request scratch directories.
func WithTempDir(dir string) AdapterOption {
	return func(a *Adapter) { a.tmpDir = dir }
}

// New returns an Adapter for cfg.
func New(cfg Config, opts ...AdapterOption) (*Adapter, error) {
	cfg, err := cfg.withDefaults()
	if err != nil {
		return nil, err
	}
	a := &Adapter{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.Timeout},
		runner: ExecRunner{},
	}
	for _, opt := range opts {
		opt(a)
	}
	return a, nil
}

// NewFromOptions is the vision.Factory for ProviderName.
func NewFromOptions(opts vision.Options) (vision.Analyzer, error) {
	cfg, err := configFromOptions(opts)
	if err != nil {
		return nil, err
	}
	return New(cfg)
}

// Model returns the descriptive model label recorded in provenance.
func (a *Adapter) Model() string {
	return a.cfg.Model
}

// Analyze extracts and analyzes every frame of req via OpenAI.
func (a *Adapter) Analyze(ctx context.Context, req vision.Request) (vision.Analysis, error) {
	segmentID := req.Segment.SegmentID
	fail := func(step Step, frame *int64, stderr []byte, err error) (vision.Analysis, error) {
		return vision.Analysis{}, &Error{Step: step, SegmentID: segmentID, FrameMs: frame, Stderr: strings.TrimSpace(string(stderr)), Err: err}
	}
	switch req.Source.Kind {
	case vision.SourceLocal, vision.SourceFixture:
	default:
		return fail(StepSource, nil, nil, fmt.Errorf("%w: kind %q (%s); only LOCAL and FIXTURE media files can be read",
			vision.ErrUnsupportedSource, req.Source.Kind, req.Source.URI))
	}
	if req.Source.Path == "" {
		return fail(StepSource, nil, nil, fmt.Errorf("%w: %s media %q has no host path", vision.ErrUnsupportedSource, req.Source.Kind, req.Source.URI))
	}
	window := req.Segment.Window
	if window.StartMs < 0 || window.EndMs <= window.StartMs {
		return fail(StepSource, nil, nil, fmt.Errorf("invalid window [%d, %d]", window.StartMs, window.EndMs))
	}
	if len(req.FrameTimesMs) == 0 {
		return fail(StepSource, nil, nil, fmt.Errorf("no frames requested"))
	}
	for _, t := range req.FrameTimesMs {
		if t < window.StartMs || t > window.EndMs {
			return fail(StepSource, &t, nil, fmt.Errorf("frame outside window [%d, %d]", window.StartMs, window.EndMs))
		}
	}
	if err := ctx.Err(); err != nil {
		return vision.Analysis{}, err
	}
	input, err := filepath.Abs(req.Source.Path)
	if err != nil {
		return fail(StepSource, nil, nil, err)
	}

	dir, err := os.MkdirTemp(a.tmpDir, "openai-vision-*")
	if err != nil {
		return fail(StepWorkspace, nil, nil, err)
	}
	defer os.RemoveAll(dir)

	frames := make([]vision.Frame, 0, len(req.FrameTimesMs))
	for _, t := range req.FrameTimesMs {
		if err := ctx.Err(); err != nil {
			return vision.Analysis{}, err
		}
		image := filepath.Join(dir, fmt.Sprintf("frame-%d.jpg", t))
		if stderr, err := a.runner.Run(ctx, a.cfg.FFmpeg, a.ffmpegArgs(input, t, image)); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return vision.Analysis{}, ctxErr
			}
			return fail(StepExtract, &t, stderr, fmt.Errorf("%s: %w", a.cfg.FFmpeg, err))
		}
		content, err := a.analyzeImage(ctx, image, FramePrompt(frames))
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return vision.Analysis{}, ctxErr
			}
			return fail(StepAnalyze, &t, nil, err)
		}
		frame, err := parseAnswer(content, t)
		if err != nil {
			return vision.Analysis{}, &Error{Step: StepParse, SegmentID: segmentID, FrameMs: &t, Output: excerpt(content), Err: err}
		}
		frames = append(frames, frame)
	}
	return vision.Analysis{Frames: frames, Provider: ProviderName, Model: a.Model()}, nil
}

func (a *Adapter) ffmpegArgs(input string, timestampMs int64, image string) []string {
	edge := strconv.Itoa(a.cfg.MaxEdge)
	return []string{
		"-nostdin", "-hide_banner", "-loglevel", "error",
		"-protocol_whitelist", "file",
		"-ss", seconds(timestampMs),
		"-i", "file:" + input,
		"-frames:v", "1", "-an",
		"-vf", "scale=w=" + edge + ":h=" + edge + ":force_original_aspect_ratio=decrease",
		"-q:v", "2",
		"-y", image,
	}
}

func seconds(ms int64) string {
	return fmt.Sprintf("%d.%03d", ms/1000, ms%1000)
}

var _ vision.Analyzer = (*Adapter)(nil)
