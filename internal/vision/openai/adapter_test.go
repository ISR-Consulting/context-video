package openai

import (
	"context"
	"encoding/json"
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

	"github.com/ISR-Consulting/context-video/internal/vision"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

type call struct {
	name string
	args []string
}

type fakeRunner struct {
	mu    sync.Mutex
	calls []call
	fail  error
	hook  func(name string)
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
		return []byte("ffmpeg fail"), f.fail
	}
	if i := indexOf(args, "-y"); i >= 0 && i+1 < len(args) {
		if err := os.WriteFile(args[i+1], []byte{0xff, 0xd8, 0xff}, 0o600); err != nil {
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

func request(kind vision.SourceKind, path string, times ...int64) vision.Request {
	if len(times) == 0 {
		times = []int64{143250, 145750}
	}
	return vision.Request{
		Segment: contracts.MediaSegment{
			SegmentID: "live-xyz:142000-147000",
			Content:   contracts.ContentRef{ContentID: "live-xyz", ContentType: contracts.ContentTypeLive},
			Window:    contracts.TimeWindow{StartMs: 142000, EndMs: 147000},
		},
		Source:       vision.Source{Kind: kind, URI: "media/match.mp4", Path: path},
		FrameTimesMs: times,
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
	a, err := New(cfg, WithHTTPClient(srv.Client()), WithRunner(runner), WithTempDir(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAnalyzeMapsJSON(t *testing.T) {
	var bodies []map[string]any
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, body)
		frameJSON := `{"description":"a football jersey on a pitch","observations":[{"type":"OBJECT","value":"football_jersey","confidence":0.91},{"type":"ENTITY","value":"Palmeiras","confidence":0.87}]}`
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + jsonQuote(frameJSON) + `}}]}`))
	}
	a := newTestAdapter(t, handler, &fakeRunner{}, Config{Detail: "low"})
	got, err := a.Analyze(context.Background(), request(vision.SourceLocal, filepath.Join(t.TempDir(), "match.mp4"), 143250))
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != ProviderName || got.Model != defaultModel || len(got.Frames) != 1 {
		t.Fatalf("%+v", got)
	}
	if got.Frames[0].Description == "" || len(got.Frames[0].Detections) != 2 {
		t.Fatalf("frame %+v", got.Frames[0])
	}
	if bodies[0]["model"] != defaultModel {
		t.Fatalf("model %v", bodies[0]["model"])
	}
	msgs := bodies[0]["messages"].([]any)
	content := msgs[0].(map[string]any)["content"].([]any)
	img := content[1].(map[string]any)["image_url"].(map[string]any)
	if img["detail"] != "low" {
		t.Fatalf("detail %v", img["detail"])
	}
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestAnalyzeHTTPError(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"nope"}`, http.StatusUnauthorized)
	}
	a := newTestAdapter(t, handler, &fakeRunner{}, Config{})
	_, err := a.Analyze(context.Background(), request(vision.SourceLocal, filepath.Join(t.TempDir(), "m.mp4"), 143250))
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("err %v", err)
	}
}

func TestAnalyzeCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := newTestAdapter(t, func(http.ResponseWriter, *http.Request) {}, &fakeRunner{}, Config{})
	_, err := a.Analyze(ctx, request(vision.SourceLocal, filepath.Join(t.TempDir(), "m.mp4"), 143250))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
}

func TestAnalyzeUnsupportedSource(t *testing.T) {
	a := newTestAdapter(t, func(http.ResponseWriter, *http.Request) {}, &fakeRunner{}, Config{})
	_, err := a.Analyze(context.Background(), request(vision.SourceControlledSource, "", 143250))
	if !errors.Is(err, vision.ErrUnsupportedSource) {
		t.Fatalf("err %v", err)
	}
}

func TestAnalyzeParseError(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"description\":\"x\",\"observations\":[{\"type\":\"OBJECT\",\"value\":\"Bad\",\"confidence\":0.5}]}"}}]}`))
	}
	a := newTestAdapter(t, handler, &fakeRunner{}, Config{})
	_, err := a.Analyze(context.Background(), request(vision.SourceLocal, filepath.Join(t.TempDir(), "m.mp4"), 143250))
	var ae *Error
	if !errors.As(err, &ae) || ae.Step != StepParse {
		t.Fatalf("err %v", err)
	}
}

func TestNewFromOptions(t *testing.T) {
	t.Setenv(envAPIKey, "")
	_, err := NewFromOptions(vision.Options{})
	if err == nil || !strings.Contains(err.Error(), envAPIKey) {
		t.Fatalf("err %v", err)
	}
	got, err := NewFromOptions(vision.Options{OptionAPIKey: "k", OptionDetail: "high"})
	if err != nil {
		t.Fatal(err)
	}
	if got.(*Adapter).cfg.Detail != "high" || got.(*Adapter).cfg.Model != defaultModel {
		t.Fatalf("%+v", got.(*Adapter).cfg)
	}
	_, err = NewFromOptions(vision.Options{OptionAPIKey: "k", OptionDetail: "medium"})
	if err == nil || !strings.Contains(err.Error(), "detail") {
		t.Fatalf("err %v", err)
	}
}

func TestParseAnswerStrict(t *testing.T) {
	frame, err := parseAnswer([]byte(`{"description":"pitch","observations":[{"type":"OBJECT","value":"ball","confidence":0.9}]}`), 100)
	if err != nil {
		t.Fatal(err)
	}
	if frame.TimestampMs != 100 || frame.Description != "pitch" || len(frame.Detections) != 1 {
		t.Fatalf("%+v", frame)
	}
	if !reflect.DeepEqual(frame.Detections[0].Type, contracts.VisualDetectionObject) {
		t.Fatal(frame.Detections[0])
	}
}
