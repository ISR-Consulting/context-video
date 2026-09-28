package whispercpp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ISR-Consulting/context-video/internal/audio"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

type call struct {
	name string
	args []string
}

// fakeRunner records commands and emulates ffmpeg and whisper-cli: whisper
// writes json to the -of prefix unless json is nil.
type fakeRunner struct {
	mu     sync.Mutex
	calls  []call
	json   []byte
	fail   map[string]error
	stderr map[string]string
	hook   func(name string)
}

func (f *fakeRunner) Run(ctx context.Context, name string, args []string) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, call{name: name, args: slices.Clone(args)})
	f.mu.Unlock()
	if f.hook != nil {
		f.hook(name)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := f.fail[name]; err != nil {
		return []byte(f.stderr[name]), err
	}
	if i := slices.Index(args, "-of"); i >= 0 && f.json != nil {
		if err := os.WriteFile(args[i+1]+".json", f.json, 0o600); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func request(kind audio.SourceKind, path string) audio.Request {
	return audio.Request{
		Segment: contracts.MediaSegment{
			SegmentID: "live-xyz:142000-147000",
			Content:   contracts.ContentRef{ContentID: "live-xyz", ContentType: contracts.ContentTypeLive},
			Window:    contracts.TimeWindow{StartMs: 142000, EndMs: 147000},
		},
		Source: audio.Source{Kind: kind, URI: "media/match.mp4", Path: path},
	}
}

func newAdapter(t *testing.T, cfg Config, runner CommandRunner) (*Adapter, string) {
	t.Helper()
	tmp := t.TempDir()
	a, err := New(cfg, WithRunner(runner), WithTempDir(tmp))
	if err != nil {
		t.Fatal(err)
	}
	return a, tmp
}

func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("scratch files left behind: %v", entries)
	}
}

func TestTranscribeRunsFFmpegThenWhisper(t *testing.T) {
	runner := &fakeRunner{json: fixture(t, "pt-two-segments.json")}
	cfg := Config{
		Model:    "/models/ggml-large-v3-turbo.bin",
		Binary:   "/opt/homebrew/bin/whisper-cli",
		FFmpeg:   "/opt/homebrew/bin/ffmpeg",
		Language: "pt",
		Threads:  4,
	}
	a, tmp := newAdapter(t, cfg, runner)
	got, err := a.Transcribe(context.Background(), request(audio.SourceLocal, "/data/media/match.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	want := audio.Transcription{
		Text:     "Essa é a nova camisa do Palmeiras.",
		Language: "pt",
		Provider: ProviderName,
		Model:    "ggml-large-v3-turbo.bin",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("transcription:\n%+v\nwant:\n%+v", got, want)
	}
	if got.Confidence != nil {
		t.Fatal("confidence fabricated")
	}

	if len(runner.calls) != 2 {
		t.Fatalf("calls: %+v", runner.calls)
	}
	ffmpeg, whisper := runner.calls[0], runner.calls[1]
	if ffmpeg.name != cfg.FFmpeg || whisper.name != cfg.Binary {
		t.Fatalf("binaries: %q %q", ffmpeg.name, whisper.name)
	}
	wav := ffmpeg.args[len(ffmpeg.args)-1]
	scratch := filepath.Dir(wav)
	if filepath.Dir(scratch) != tmp || filepath.Base(wav) != "window.wav" {
		t.Fatalf("wav path %q not under scratch dir in %q", wav, tmp)
	}
	wantFFmpeg := []string{
		"-nostdin", "-hide_banner", "-loglevel", "error",
		"-protocol_whitelist", "file",
		"-ss", "142.000", "-t", "5.000",
		"-i", "file:/data/media/match.mp4",
		"-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "-f", "wav",
		"-y", wav,
	}
	if !reflect.DeepEqual(ffmpeg.args, wantFFmpeg) {
		t.Fatalf("ffmpeg args:\n%q\nwant:\n%q", ffmpeg.args, wantFFmpeg)
	}
	wantWhisper := []string{
		"-m", cfg.Model, "-f", wav, "-l", "pt", "-oj",
		"-of", filepath.Join(scratch, "transcript"), "-np", "-t", "4",
	}
	if !reflect.DeepEqual(whisper.args, wantWhisper) {
		t.Fatalf("whisper args:\n%q\nwant:\n%q", whisper.args, wantWhisper)
	}
	assertEmptyDir(t, tmp)
}

func TestTranscribeDefaults(t *testing.T) {
	runner := &fakeRunner{json: []byte(`{"transcription":[]}`)}
	a, _ := newAdapter(t, Config{Model: "models/ggml-tiny.bin"}, runner)
	got, err := a.Transcribe(context.Background(), request(audio.SourceFixture, "rel/clip.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "" || got.Language != "" || got.Model != "ggml-tiny.bin" {
		t.Fatalf("transcription: %+v", got)
	}
	if runner.calls[0].name != "ffmpeg" || runner.calls[1].name != "whisper-cli" {
		t.Fatalf("default binaries: %q %q", runner.calls[0].name, runner.calls[1].name)
	}
	abs, err := filepath.Abs("rel/clip.wav")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(runner.calls[0].args, "file:"+abs) {
		t.Fatalf("relative input not made absolute: %q", runner.calls[0].args)
	}
	whisper := runner.calls[1].args
	if i := slices.Index(whisper, "-l"); whisper[i+1] != "auto" {
		t.Fatalf("default language: %q", whisper)
	}
	if slices.Contains(whisper, "-t") {
		t.Fatalf("threads passed by default: %q", whisper)
	}
}

func TestTranscribeLanguageFallback(t *testing.T) {
	cases := []struct {
		name, json, requested, want string
	}{
		{"reported wins", `{"result":{"language":"es"},"transcription":[]}`, "pt", "es"},
		{"requested used when not reported", `{"transcription":[]}`, "pt", "pt"},
		{"auto is never reported", `{"result":{"language":""},"transcription":[]}`, "auto", ""},
		{"detected with auto", `{"result":{"language":"pt"},"transcription":[]}`, "auto", "pt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := newAdapter(t, Config{Model: "m.bin", Language: tc.requested}, &fakeRunner{json: []byte(tc.json)})
			got, err := a.Transcribe(context.Background(), request(audio.SourceLocal, "/m.mp4"))
			if err != nil {
				t.Fatal(err)
			}
			if got.Language != tc.want {
				t.Fatalf("language %q want %q", got.Language, tc.want)
			}
		})
	}
}

func TestTranscribeWindowSeconds(t *testing.T) {
	for _, tc := range []struct {
		start, end int64
		ss, dur    string
	}{
		{0, 2000, "0.000", "2.000"},
		{5000, 7500, "5.000", "2.500"},
		{9999, 10000, "9.999", "0.001"},
	} {
		runner := &fakeRunner{json: []byte(`{"transcription":[]}`)}
		a, _ := newAdapter(t, Config{Model: "m.bin"}, runner)
		req := request(audio.SourceLocal, "/m.mp4")
		req.Segment.Window = contracts.TimeWindow{StartMs: tc.start, EndMs: tc.end}
		if _, err := a.Transcribe(context.Background(), req); err != nil {
			t.Fatal(err)
		}
		args := runner.calls[0].args
		if ss := args[slices.Index(args, "-ss")+1]; ss != tc.ss {
			t.Errorf("-ss %q want %q", ss, tc.ss)
		}
		if dur := args[slices.Index(args, "-t")+1]; dur != tc.dur {
			t.Errorf("-t %q want %q", dur, tc.dur)
		}
	}
}

func TestTranscribeRejectsUnsupportedSources(t *testing.T) {
	for name, req := range map[string]audio.Request{
		"controlled source":        request(audio.SourceControlledSource, ""),
		"controlled source w/path": request(audio.SourceControlledSource, "/x"),
		"unknown kind":             request("S3", "/x"),
		"local without path":       request(audio.SourceLocal, ""),
	} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{json: []byte(`{"transcription":[]}`)}
			a, _ := newAdapter(t, Config{Model: "m.bin"}, runner)
			_, err := a.Transcribe(context.Background(), req)
			if !errors.Is(err, audio.ErrUnsupportedSource) {
				t.Fatalf("expected ErrUnsupportedSource, got %v", err)
			}
			var adapterErr *Error
			if !errors.As(err, &adapterErr) || adapterErr.Step != StepSource || adapterErr.SegmentID != req.Segment.SegmentID {
				t.Fatalf("error: %#v", err)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("commands executed: %+v", runner.calls)
			}
		})
	}
	_, err := func() (audio.Transcription, error) {
		a, _ := newAdapter(t, Config{Model: "m.bin"}, &fakeRunner{})
		return a.Transcribe(context.Background(), request(audio.SourceControlledSource, ""))
	}()
	if msg := err.Error(); !strings.Contains(msg, "CONTROLLED_SOURCE") || !strings.Contains(msg, "media/match.mp4") {
		t.Fatalf("error not descriptive: %s", msg)
	}
}

func TestTranscribeCommandFailures(t *testing.T) {
	cause := errors.New("exit status 1")
	cases := []struct {
		name   string
		runner *fakeRunner
		step   Step
		want   string
	}{
		{
			name: "ffmpeg fails",
			runner: &fakeRunner{fail: map[string]error{"ffmpeg": cause},
				stderr: map[string]string{"ffmpeg": "Invalid data found when processing input\n"}},
			step: StepExtract, want: "Invalid data found",
		},
		{
			name: "whisper fails",
			runner: &fakeRunner{fail: map[string]error{"whisper-cli": cause},
				stderr: map[string]string{"whisper-cli": "failed to load model"}},
			step: StepTranscribe, want: "failed to load model",
		},
		{name: "no json written", runner: &fakeRunner{}, step: StepParse, want: "read whisper-cli output"},
		{name: "malformed json", runner: &fakeRunner{json: []byte("{")}, step: StepParse, want: "decode whisper-cli output"},
		{name: "missing transcription", runner: &fakeRunner{json: []byte(`{"result":{}}`)}, step: StepParse, want: "transcription"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, tmp := newAdapter(t, Config{Model: "m.bin"}, tc.runner)
			_, err := a.Transcribe(context.Background(), request(audio.SourceLocal, "/m.mp4"))
			var adapterErr *Error
			if !errors.As(err, &adapterErr) || adapterErr.Step != tc.step {
				t.Fatalf("step: %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "live-xyz:142000-147000") {
				t.Fatalf("error %q missing %q or segment", err, tc.want)
			}
			if tc.step == StepExtract && len(tc.runner.calls) != 1 {
				t.Fatalf("whisper ran after ffmpeg failure: %+v", tc.runner.calls)
			}
			assertEmptyDir(t, tmp)
		})
	}
}

func TestTranscribeCancellation(t *testing.T) {
	t.Run("before start", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		runner := &fakeRunner{}
		a, _ := newAdapter(t, Config{Model: "m.bin"}, runner)
		if _, err := a.Transcribe(ctx, request(audio.SourceLocal, "/m.mp4")); !errors.Is(err, context.Canceled) {
			t.Fatalf("err: %v", err)
		}
		if len(runner.calls) != 0 {
			t.Fatal("command ran after cancellation")
		}
	})
	t.Run("during extraction", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		runner := &fakeRunner{hook: func(string) { cancel() }}
		a, tmp := newAdapter(t, Config{Model: "m.bin"}, runner)
		_, err := a.Transcribe(ctx, request(audio.SourceLocal, "/m.mp4"))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err: %v", err)
		}
		if len(runner.calls) != 1 {
			t.Fatalf("calls after cancellation: %+v", runner.calls)
		}
		assertEmptyDir(t, tmp)
	})
}

func TestNewFromOptions(t *testing.T) {
	transcriber, err := NewFromOptions(audio.Options{
		"model": "/m/ggml-large-v3-turbo.bin", "binary": "/b/whisper-cli", "ffmpeg": "/b/ffmpeg", "language": "pt", "threads": "8",
	})
	if err != nil {
		t.Fatal(err)
	}
	a := transcriber.(*Adapter)
	want := Config{Model: "/m/ggml-large-v3-turbo.bin", Binary: "/b/whisper-cli", FFmpeg: "/b/ffmpeg", Language: "pt", Threads: 8}
	if a.cfg != want {
		t.Fatalf("config: %+v", a.cfg)
	}
	if _, ok := a.runner.(ExecRunner); !ok {
		t.Fatalf("runner: %T", a.runner)
	}

	for name, tc := range map[string]struct {
		opts audio.Options
		want string
	}{
		"missing model":  {audio.Options{}, `option "model"`},
		"blank model":    {audio.Options{"model": " "}, `option "model"`},
		"unknown option": {audio.Options{"model": "m", "modle": "x"}, `unknown option "modle"`},
		"bad threads":    {audio.Options{"model": "m", "threads": "many"}, `option "threads"`},
		"zero threads":   {audio.Options{"model": "m", "threads": "0"}, `option "threads"`},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewFromOptions(tc.opts); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v does not contain %q", err, tc.want)
			}
		})
	}
}

func TestTailBufferKeepsLastBytes(t *testing.T) {
	var b tailBuffer
	_, _ = b.Write([]byte(strings.Repeat("a", maxStderr)))
	_, _ = b.Write([]byte("tail"))
	got := string(b.Bytes())
	if len(got) != maxStderr || !strings.HasSuffix(got, "tail") {
		t.Fatalf("len %d suffix %q", len(got), got[len(got)-8:])
	}
}

func TestExecRunner(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}
	stderr, err := ExecRunner{}.Run(context.Background(), sh, []string{"-c", "echo oops >&2; exit 3"})
	if err == nil || strings.TrimSpace(string(stderr)) != "oops" {
		t.Fatalf("stderr %q err %v", stderr, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (ExecRunner{}).Run(ctx, sh, []string{"-c", "sleep 5"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled run: %v", err)
	}
}
