package openai

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ISR-Consulting/context-video/internal/audio"
)

// ProviderName is the registry name and provenance provider of this adapter.
const ProviderName = "openai-transcribe"

// Step identifies which part of a transcription failed.
type Step string

const (
	StepSource     Step = "source"
	StepWorkspace  Step = "workspace"
	StepExtract    Step = "extract"
	StepTranscribe Step = "transcribe"
)

// Error is a step-qualified adapter failure.
type Error struct {
	Step      Step
	SegmentID string
	Stderr    string
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
	if e.Err != nil {
		parts = append(parts, e.Err.Error())
	}
	msg := strings.Join(parts, ": ")
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

// Adapter transcribes media windows with ffmpeg and OpenAI transcriptions.
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
	if a.client.Timeout == 0 && cfg.Timeout > 0 {
		a.client.Timeout = cfg.Timeout
	}
	return a, nil
}

// NewFromOptions is the audio.Factory for ProviderName.
func NewFromOptions(opts audio.Options) (audio.Transcriber, error) {
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

// Transcribe extracts req.Segment.Window from the local media file and
// transcribes it via OpenAI.
func (a *Adapter) Transcribe(ctx context.Context, req audio.Request) (audio.Transcription, error) {
	segmentID := req.Segment.SegmentID
	fail := func(step Step, stderr []byte, err error) (audio.Transcription, error) {
		return audio.Transcription{}, &Error{Step: step, SegmentID: segmentID, Stderr: strings.TrimSpace(string(stderr)), Err: err}
	}
	switch req.Source.Kind {
	case audio.SourceLocal, audio.SourceFixture:
	default:
		return fail(StepSource, nil, fmt.Errorf("%w: kind %q (%s); only LOCAL and FIXTURE media files can be read",
			audio.ErrUnsupportedSource, req.Source.Kind, req.Source.URI))
	}
	if req.Source.Path == "" {
		return fail(StepSource, nil, fmt.Errorf("%w: %s media %q has no host path", audio.ErrUnsupportedSource, req.Source.Kind, req.Source.URI))
	}
	window := req.Segment.Window
	if window.StartMs < 0 || window.EndMs <= window.StartMs {
		return fail(StepSource, nil, fmt.Errorf("invalid window [%d, %d]", window.StartMs, window.EndMs))
	}
	if err := ctx.Err(); err != nil {
		return audio.Transcription{}, err
	}
	input, err := filepath.Abs(req.Source.Path)
	if err != nil {
		return fail(StepSource, nil, err)
	}

	dir, err := os.MkdirTemp(a.tmpDir, "openai-audio-*")
	if err != nil {
		return fail(StepWorkspace, nil, err)
	}
	defer os.RemoveAll(dir)

	wav := filepath.Join(dir, "window.wav")
	if stderr, err := a.runner.Run(ctx, a.cfg.FFmpeg, a.ffmpegArgs(input, window.StartMs, window.EndMs, wav)); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return audio.Transcription{}, ctxErr
		}
		return fail(StepExtract, stderr, fmt.Errorf("%s: %w", a.cfg.FFmpeg, err))
	}
	out, err := a.transcribeFile(ctx, wav)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return audio.Transcription{}, ctxErr
		}
		return fail(StepTranscribe, nil, err)
	}
	language := strings.TrimSpace(out.Language)
	if language == "" && a.cfg.Language != "" && a.cfg.Language != "auto" {
		language = a.cfg.Language
	}
	return audio.Transcription{
		Text:     out.Text,
		Language: language,
		Provider: ProviderName,
		Model:    a.Model(),
	}, nil
}

func (a *Adapter) ffmpegArgs(input string, startMs, endMs int64, wav string) []string {
	return []string{
		"-nostdin", "-hide_banner", "-loglevel", "error",
		"-protocol_whitelist", "file",
		"-ss", seconds(startMs),
		"-t", seconds(endMs - startMs),
		"-i", "file:" + input,
		"-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "-f", "wav",
		"-y", wav,
	}
}

func seconds(ms int64) string {
	return fmt.Sprintf("%d.%03d", ms/1000, ms%1000)
}

var _ audio.Transcriber = (*Adapter)(nil)
