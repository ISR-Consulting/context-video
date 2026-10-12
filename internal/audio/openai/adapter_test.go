package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ISR-Consulting/context-video/internal/audio"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

type call struct {
	name string
	args []string
}

type fakeRunner struct {
	mu     sync.Mutex
	calls  []call
	fail   error
	stderr string
	hook   func(name string)
}

func (f *fakeRunner) Run(ctx context.Context, name string, args []string) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, call{name: name, args: append([]string(nil), args...)})
	f.mu.Unlock()
	if f.hook != nil {
		f.hook(name)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.fail != nil {
		return []byte(f.stderr), f.fail
	}
	// Write a tiny WAV so the HTTP multipart step can open it.
	if i := indexOf(args, "-y"); i >= 0 && i+1 < len(args) {
		if err := os.WriteFile(args[i+1], []byte("RIFF....WAVEfmt "), 0o600); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func indexOf(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
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

func newTestAdapter(t *testing.T, handler http.HandlerFunc, runner CommandRunner, cfg Config) *Adapter {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	if cfg.APIKey == "" {
		cfg.APIKey = "test-key"
	}
	cfg.BaseURL = srv.URL
	if cfg.Model == "" {
		cfg.Model = defaultModel
	}
	a, err := New(cfg, WithHTTPClient(srv.Client()), WithRunner(runner), WithTempDir(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestTranscribeMapsJSON(t *testing.T) {
	var gotAuth, gotContentType string
	var gotBody strings.Builder
	handler := func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		if r.URL.Path != "/audio/transcriptions" {
			t.Errorf("path %s", r.URL.Path)
		}
		_, _ = io.Copy(&gotBody, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"Essa é a nova camisa do Palmeiras.","language":"pt"}`))
	}
	runner := &fakeRunner{}
	a := newTestAdapter(t, handler, runner, Config{Language: "pt", FFmpeg: "ffmpeg"})
	got, err := a.Transcribe(context.Background(), request(audio.SourceLocal, filepath.Join(t.TempDir(), "match.mp4")))
	if err != nil {
		t.Fatal(err)
	}
	want := audio.Transcription{
		Text:     "Essa é a nova camisa do Palmeiras.",
		Language: "pt",
		Provider: ProviderName,
		Model:    defaultModel,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("auth %q", gotAuth)
	}
	if !strings.HasPrefix(gotContentType, "multipart/form-data") {
		t.Fatalf("content-type %q", gotContentType)
	}
	if !strings.Contains(gotBody.String(), defaultModel) {
		t.Fatalf("body missing model: %s", gotBody.String())
	}
	if len(runner.calls) != 1 || runner.calls[0].name != "ffmpeg" {
		t.Fatalf("ffmpeg calls: %+v", runner.calls)
	}
}

func TestTranscribeHTTPError(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"bad request"}}`, http.StatusBadRequest)
	}
	a := newTestAdapter(t, handler, &fakeRunner{}, Config{})
	_, err := a.Transcribe(context.Background(), request(audio.SourceLocal, filepath.Join(t.TempDir(), "m.mp4")))
	if err == nil || !strings.Contains(err.Error(), "HTTP 400") {
		t.Fatalf("err %v", err)
	}
	var ae *Error
	if !errors.As(err, &ae) || ae.Step != StepTranscribe {
		t.Fatalf("step: %v", err)
	}
}

func TestTranscribeCancel(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := newTestAdapter(t, handler, &fakeRunner{hook: func(string) {}}, Config{})
	_, err := a.Transcribe(ctx, request(audio.SourceLocal, filepath.Join(t.TempDir(), "m.mp4")))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
}

func TestTranscribeUnsupportedSource(t *testing.T) {
	a := newTestAdapter(t, func(http.ResponseWriter, *http.Request) {}, &fakeRunner{}, Config{})
	_, err := a.Transcribe(context.Background(), request(audio.SourceControlledSource, ""))
	if !errors.Is(err, audio.ErrUnsupportedSource) {
		t.Fatalf("err %v", err)
	}
}

func TestNewFromOptions(t *testing.T) {
	t.Setenv(envAPIKey, "")
	_, err := NewFromOptions(audio.Options{})
	if err == nil || !strings.Contains(err.Error(), envAPIKey) {
		t.Fatalf("err %v", err)
	}
	got, err := NewFromOptions(audio.Options{
		OptionAPIKey:  "k",
		OptionModel:   "gpt-4o-mini-transcribe",
		OptionTimeout: "30s",
		OptionBaseURL: "https://example.test/v1/",
	})
	if err != nil {
		t.Fatal(err)
	}
	a := got.(*Adapter)
	if a.cfg.BaseURL != "https://example.test/v1" || a.cfg.Timeout != 30*time.Second {
		t.Fatalf("cfg %+v", a.cfg)
	}
	_, err = NewFromOptions(audio.Options{OptionAPIKey: "k", "modle": "x"})
	if err == nil || !strings.Contains(err.Error(), "unknown option") {
		t.Fatalf("err %v", err)
	}
}

func TestNewFromOptionsUsesEnv(t *testing.T) {
	t.Setenv(envAPIKey, "env-key")
	got, err := NewFromOptions(audio.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got.(*Adapter).cfg.APIKey != "env-key" {
		t.Fatal(got.(*Adapter).cfg.APIKey)
	}
}
