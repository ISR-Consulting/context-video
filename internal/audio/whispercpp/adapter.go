package whispercpp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ISR-Consulting/context-video/internal/audio"
)

// ProviderName is the registry name and provenance provider of this adapter.
const ProviderName = "whisper-cpp"

// Step identifies which part of a transcription failed.
type Step string

const (
	StepSource     Step = "source"
	StepWorkspace  Step = "workspace"
	StepExtract    Step = "extract"
	StepTranscribe Step = "transcribe"
	StepParse      Step = "parse"
)

// Error is a step-qualified adapter failure. Stderr holds the tail of the
// failing command's stderr, when there is one.
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

// Adapter transcribes media windows with ffmpeg and whisper-cli.
type Adapter struct {
	cfg    Config
	runner CommandRunner
	tmpDir string
}

// AdapterOption customizes an Adapter.
type AdapterOption func(*Adapter)

// WithRunner replaces the os/exec command runner.
func WithRunner(r CommandRunner) AdapterOption {
	return func(a *Adapter) {
		if r != nil {
			a.runner = r
		}
	}
}

// WithTempDir sets the parent directory for per-request scratch directories.
// The default is os.TempDir.
func WithTempDir(dir string) AdapterOption {
	return func(a *Adapter) { a.tmpDir = dir }
}

// New returns an Adapter for cfg. Model is required; other fields default to
// whisper-cli, ffmpeg and automatic language detection.
func New(cfg Config, opts ...AdapterOption) (*Adapter, error) {
	cfg, err := cfg.withDefaults()
	if err != nil {
		return nil, err
	}
	a := &Adapter{cfg: cfg, runner: ExecRunner{}}
	for _, opt := range opts {
		opt(a)
	}
	return a, nil
}

// NewFromOptions is the audio.Factory for ProviderName. Supported keys are
// model (required), binary, ffmpeg, language and threads.
func NewFromOptions(opts audio.Options) (audio.Transcriber, error) {
	cfg, err := configFromOptions(opts)
	if err != nil {
		return nil, err
	}
	return New(cfg)
}

// Model returns the descriptive model label recorded in provenance: the model
// file name without its directory.
func (a *Adapter) Model() string {
	return filepath.Base(a.cfg.Model)
}

// Transcribe extracts req.Segment.Window from the local media file and
// transcribes it. CONTROLLED_SOURCE media, or a source without a host path,
// returns an error wrapping audio.ErrUnsupportedSource without running any
// command.
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

	dir, err := os.MkdirTemp(a.tmpDir, "whispercpp-*")
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
	prefix := filepath.Join(dir, "transcript")
	if stderr, err := a.runner.Run(ctx, a.cfg.Binary, a.whisperArgs(wav, prefix)); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return audio.Transcription{}, ctxErr
		}
		return fail(StepTranscribe, stderr, fmt.Errorf("%s: %w", a.cfg.Binary, err))
	}
	data, err := os.ReadFile(prefix + ".json")
	if err != nil {
		return fail(StepParse, nil, fmt.Errorf("read whisper-cli output: %w", err))
	}
	out, err := parseOutput(data)
	if err != nil {
		return fail(StepParse, nil, fmt.Errorf("decode whisper-cli output: %w", err))
	}

	language := out.language
	if language == "" && a.cfg.Language != defaultLanguage {
		language = a.cfg.Language
	}
	return audio.Transcription{
		Text:     out.text,
		Language: language,
		Provider: ProviderName,
		Model:    a.Model(),
	}, nil
}

// ffmpegArgs extracts [startMs, endMs] of input to 16 kHz mono PCM WAV. The
// file protocol whitelist and file: prefix prevent any network input.
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

func (a *Adapter) whisperArgs(wav, prefix string) []string {
	args := []string{
		"-m", a.cfg.Model,
		"-f", wav,
		"-l", a.cfg.Language,
		"-oj",
		"-of", prefix,
		"-np",
	}
	if a.cfg.Threads > 0 {
		args = append(args, "-t", strconv.Itoa(a.cfg.Threads))
	}
	return args
}

// seconds formats milliseconds as exact decimal seconds, e.g. 142000 → "142.000".
func seconds(ms int64) string {
	return fmt.Sprintf("%d.%03d", ms/1000, ms%1000)
}

var _ audio.Transcriber = (*Adapter)(nil)
