package llamamtmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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

// fakeRunner records commands and emulates ffmpeg and llama-mtmd-cli: every
// llama call returns the next entry of answers (the last one repeats).
type fakeRunner struct {
	mu      sync.Mutex
	calls   []call
	answers []string
	fail    map[string]error
	stderr  map[string]string
	hook    func(name string)
	llama   int
}

func (f *fakeRunner) Run(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, call{name: name, args: slices.Clone(args)})
	f.mu.Unlock()
	if f.hook != nil {
		f.hook(name)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if err := f.fail[name]; err != nil {
		return nil, []byte(f.stderr[name]), err
	}
	if !slices.Contains(args, "--mmproj") {
		return nil, nil, nil
	}
	answer := ""
	if len(f.answers) > 0 {
		answer = f.answers[min(f.llama, len(f.answers)-1)]
	}
	f.llama++
	return []byte(answer), []byte("llama logs"), nil
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
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

func baseConfig() Config {
	return Config{Model: "/models/Qwen2.5-VL-7B-Instruct-Q4_K_M.gguf", MMProj: "/models/mmproj-Qwen2.5-VL-7B-Instruct-f16.gguf"}
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

func ptr[T any](v T) *T { return &v }

func TestAnalyzeRunsFFmpegThenLlamaPerFrame(t *testing.T) {
	runner := &fakeRunner{answers: []string{fixture(t, "jersey.json"), fixture(t, "fenced-empty.txt")}}
	cfg := baseConfig()
	cfg.Binary = "/opt/homebrew/bin/llama-mtmd-cli"
	cfg.FFmpeg = "/opt/homebrew/bin/ffmpeg"
	cfg.Threads = 8
	cfg.GPULayers = ptr(99)
	cfg.MaxTokens = 256
	cfg.MaxEdge = 512
	a, tmp := newAdapter(t, cfg, runner)

	got, err := a.Analyze(context.Background(), request(vision.SourceLocal, "/data/media/match.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	want := vision.Analysis{
		Frames: []vision.Frame{
			{
				TimestampMs: 143250,
				Description: "A player wearing a green Palmeiras jersey in a stadium.",
				Detections: []vision.Detection{
					{Type: contracts.VisualDetectionObject, Value: "football_jersey", Confidence: ptr(0.92)},
					{Type: contracts.VisualDetectionEntity, Value: "Palmeiras", Confidence: ptr(0.81)},
					{Type: contracts.VisualDetectionScene, Value: "stadium", Confidence: ptr(0.9)},
					{Type: contracts.VisualDetectionText, Value: "PALMEIRAS 2 x 1 SANTOS", Confidence: ptr(0.7)},
				},
			},
			{TimestampMs: 145750, Description: "A dark frame.", Detections: []vision.Detection{}},
		},
		Provider: ProviderName,
		Model:    "Qwen2.5-VL-7B-Instruct-Q4_K_M.gguf",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("analysis:\n%+v\nwant:\n%+v", got, want)
	}

	if len(runner.calls) != 4 {
		t.Fatalf("calls: %d", len(runner.calls))
	}
	for i, frame := range []struct {
		ms int64
		ss string
	}{{143250, "143.250"}, {145750, "145.750"}} {
		ffmpeg, llama := runner.calls[2*i], runner.calls[2*i+1]
		if ffmpeg.name != cfg.FFmpeg || llama.name != cfg.Binary {
			t.Fatalf("binaries: %q %q", ffmpeg.name, llama.name)
		}
		image := ffmpeg.args[len(ffmpeg.args)-1]
		if filepath.Dir(filepath.Dir(image)) != tmp || filepath.Base(image) != fmt.Sprintf("frame-%d.jpg", frame.ms) {
			t.Fatalf("image path %q", image)
		}
		wantFFmpeg := []string{
			"-nostdin", "-hide_banner", "-loglevel", "error",
			"-protocol_whitelist", "file",
			"-ss", frame.ss,
			"-i", "file:/data/media/match.mp4",
			"-frames:v", "1", "-an",
			"-vf", "scale=w=512:h=512:force_original_aspect_ratio=decrease",
			"-q:v", "2",
			"-y", image,
		}
		if !reflect.DeepEqual(ffmpeg.args, wantFFmpeg) {
			t.Fatalf("ffmpeg args:\n%q\nwant:\n%q", ffmpeg.args, wantFFmpeg)
		}
		wantLlama := []string{
			"-m", cfg.Model, "--mmproj", cfg.MMProj, "--image", image,
			"-p", Prompt, "--json-schema", ResponseSchema,
			"--temp", "0", "--seed", "0", "-n", "256", "-t", "8", "-ngl", "99",
		}
		if !reflect.DeepEqual(llama.args, wantLlama) {
			t.Fatalf("llama args:\n%q\nwant:\n%q", llama.args, wantLlama)
		}
		for _, arg := range llama.args {
			if arg == "-hf" || arg == "--hf-repo" || strings.HasPrefix(arg, "http") {
				t.Fatalf("network argument %q", arg)
			}
		}
	}
	assertEmptyDir(t, tmp)
}

func TestAnalyzeDefaults(t *testing.T) {
	runner := &fakeRunner{answers: []string{`{"description":"","observations":[]}`}}
	a, _ := newAdapter(t, baseConfig(), runner)
	if _, err := a.Analyze(context.Background(), request(vision.SourceFixture, "rel/clip.mp4", 142000)); err != nil {
		t.Fatal(err)
	}
	if runner.calls[0].name != "ffmpeg" || runner.calls[1].name != "llama-mtmd-cli" {
		t.Fatalf("default binaries: %q %q", runner.calls[0].name, runner.calls[1].name)
	}
	abs, err := filepath.Abs("rel/clip.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(runner.calls[0].args, "file:"+abs) {
		t.Fatalf("relative input not made absolute: %q", runner.calls[0].args)
	}
	if !slices.Contains(runner.calls[0].args, "scale=w=768:h=768:force_original_aspect_ratio=decrease") {
		t.Fatalf("default max edge: %q", runner.calls[0].args)
	}
	llama := runner.calls[1].args
	if i := slices.Index(llama, "-n"); llama[i+1] != "512" {
		t.Fatalf("default max tokens: %q", llama)
	}
	if slices.Contains(llama, "-t") || slices.Contains(llama, "-ngl") {
		t.Fatalf("threads/gpu layers passed by default: %q", llama)
	}
}

func TestResponseSchemaMatchesContractTypes(t *testing.T) {
	var schema struct {
		Properties struct {
			Observations struct {
				Items struct {
					Required   []string `json:"required"`
					Properties struct {
						Type struct {
							Enum []contracts.VisualDetectionType `json:"enum"`
						} `json:"type"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"observations"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(ResponseSchema), &schema); err != nil {
		t.Fatal(err)
	}
	items := schema.Properties.Observations.Items
	if !reflect.DeepEqual(items.Properties.Type.Enum, vision.DetectionTypes()) {
		t.Fatalf("enum %v", items.Properties.Type.Enum)
	}
	if !slices.Contains(items.Required, "confidence") {
		t.Fatal("schema does not require confidence")
	}
	for _, typ := range vision.DetectionTypes() {
		if !strings.Contains(Prompt, string(typ)+":") {
			t.Errorf("prompt does not describe %s", typ)
		}
	}
	if PromptVersion == "" {
		t.Fatal("blank prompt version")
	}
}

func TestParseAnswer(t *testing.T) {
	ok := []struct {
		name, out string
		want      vision.Frame
	}{
		{
			name: "plain with trailing newline",
			out:  `{"description":" x ","observations":[{"type":"ACTION","value":"goal_celebration","confidence":1}]}` + "\n\n",
			want: vision.Frame{TimestampMs: 7, Description: "x", Detections: []vision.Detection{
				{Type: contracts.VisualDetectionAction, Value: "goal_celebration", Confidence: ptr(1.0)},
			}},
		},
		{
			name: "fenced",
			out:  "```json\n{\"description\":\"d\",\"observations\":[{\"type\":\"BRAND\",\"value\":\"Puma\",\"confidence\":0}]}\n```",
			want: vision.Frame{TimestampMs: 7, Description: "d", Detections: []vision.Detection{
				{Type: contracts.VisualDetectionBrand, Value: "Puma", Confidence: ptr(0.0)},
			}},
		},
		{
			name: "no description",
			out:  `{"observations":[]}`,
			want: vision.Frame{TimestampMs: 7, Detections: []vision.Detection{}},
		},
	}
	for _, tc := range ok {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseAnswer([]byte(tc.out), 7)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("frame %+v want %+v", got, tc.want)
			}
		})
	}

	bad := map[string]struct{ out, want string }{
		"empty":               {"  \n", "empty model answer"},
		"prose":               {"The frame shows a stadium.", "decode model answer"},
		"truncated":           {`{"description":"x","observations":[{"type":"OBJECT"`, "decode model answer"},
		"trailing garbage":    {`{"observations":[]} and more`, "unexpected data after"},
		"two objects":         {`{"observations":[]}{"observations":[]}`, "unexpected data after"},
		"array":               {`[{"type":"OBJECT"}]`, "decode model answer"},
		"unknown field":       {`{"observations":[],"products":["shirt"]}`, `unknown field "products"`},
		"unknown item field":  {`{"observations":[{"type":"OBJECT","value":"ball","confidence":0.5,"price":10}]}`, `unknown field "price"`},
		"missing array":       {`{"description":"x"}`, `no "observations" array`},
		"null array":          {`{"observations":null}`, `no "observations" array`},
		"unknown type":        {`{"observations":[{"type":"PRODUCT","value":"shirt","confidence":0.5}]}`, `type "PRODUCT"`},
		"lowercase type":      {`{"observations":[{"type":"object","value":"ball","confidence":0.5}]}`, `type "object"`},
		"blank value":         {`{"observations":[{"type":"OBJECT","value":" ","confidence":0.5}]}`, "blank value"},
		"missing confidence":  {`{"observations":[{"type":"OBJECT","value":"ball"}]}`, "missing confidence"},
		"null confidence":     {`{"observations":[{"type":"OBJECT","value":"ball","confidence":null}]}`, "missing confidence"},
		"string confidence":   {`{"observations":[{"type":"OBJECT","value":"ball","confidence":"high"}]}`, "decode model answer"},
		"confidence above 1":  {`{"observations":[{"type":"OBJECT","value":"ball","confidence":1.5}]}`, "outside [0, 1]"},
		"negative confidence": {`{"observations":[{"type":"OBJECT","value":"ball","confidence":-0.2}]}`, "outside [0, 1]"},
		"percent confidence":  {`{"observations":[{"type":"OBJECT","value":"ball","confidence":85}]}`, "outside [0, 1]"},
	}
	for name, tc := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := parseAnswer([]byte(tc.out), 7); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v does not contain %q", err, tc.want)
			}
		})
	}
}

func TestAnalyzeRejectsMalformedAnswer(t *testing.T) {
	runner := &fakeRunner{answers: []string{fixture(t, "jersey.json"), fixture(t, "missing-confidence.json")}}
	a, tmp := newAdapter(t, baseConfig(), runner)
	_, err := a.Analyze(context.Background(), request(vision.SourceLocal, "/m.mp4"))
	var adapterErr *Error
	if !errors.As(err, &adapterErr) || adapterErr.Step != StepParse || adapterErr.FrameMs == nil || *adapterErr.FrameMs != 145750 {
		t.Fatalf("error: %v", err)
	}
	msg := err.Error()
	for _, want := range []string{"segment live-xyz:142000-147000", "frame 145750ms", "missing confidence", `(output: {`} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q missing %q", msg, want)
		}
	}
	assertEmptyDir(t, tmp)
}

func TestAnalyzeRejectsBadRequests(t *testing.T) {
	cases := map[string]struct {
		req      vision.Request
		sentinel bool
		want     string
	}{
		"controlled source":        {request(vision.SourceControlledSource, ""), true, "CONTROLLED_SOURCE"},
		"controlled source w/path": {request(vision.SourceControlledSource, "/x"), true, "media/match.mp4"},
		"unknown kind":             {request("S3", "/x"), true, `"S3"`},
		"local without path":       {request(vision.SourceLocal, ""), true, "no host path"},
		"no frames":                {func() vision.Request { r := request(vision.SourceLocal, "/x"); r.FrameTimesMs = nil; return r }(), false, "no frames"},
		"frame outside window":     {request(vision.SourceLocal, "/x", 141999), false, "frame 141999ms"},
		"invalid window": {func() vision.Request {
			r := request(vision.SourceLocal, "/x")
			r.Segment.Window.EndMs = r.Segment.Window.StartMs
			return r
		}(), false, "invalid window"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{answers: []string{`{"observations":[]}`}}
			a, _ := newAdapter(t, baseConfig(), runner)
			_, err := a.Analyze(context.Background(), tc.req)
			if errors.Is(err, vision.ErrUnsupportedSource) != tc.sentinel {
				t.Fatalf("ErrUnsupportedSource = %v: %v", !tc.sentinel, err)
			}
			var adapterErr *Error
			if !errors.As(err, &adapterErr) || adapterErr.Step != StepSource || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error: %v", err)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("commands executed: %+v", runner.calls)
			}
		})
	}
}

func TestAnalyzeCommandFailures(t *testing.T) {
	cause := errors.New("exit status 1")
	cases := []struct {
		name   string
		runner *fakeRunner
		step   Step
		calls  int
		want   string
	}{
		{
			name: "ffmpeg fails",
			runner: &fakeRunner{fail: map[string]error{"ffmpeg": cause},
				stderr: map[string]string{"ffmpeg": "Invalid data found when processing input\n"}},
			step: StepExtract, calls: 1, want: "Invalid data found",
		},
		{
			name: "llama fails",
			runner: &fakeRunner{fail: map[string]error{"llama-mtmd-cli": cause},
				stderr: map[string]string{"llama-mtmd-cli": "failed to load model"}},
			step: StepAnalyze, calls: 2, want: "failed to load model",
		},
		{name: "empty answer", runner: &fakeRunner{}, step: StepParse, calls: 2, want: "empty model answer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, tmp := newAdapter(t, baseConfig(), tc.runner)
			_, err := a.Analyze(context.Background(), request(vision.SourceLocal, "/m.mp4"))
			var adapterErr *Error
			if !errors.As(err, &adapterErr) || adapterErr.Step != tc.step {
				t.Fatalf("step: %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "live-xyz:142000-147000") ||
				!strings.Contains(err.Error(), "frame 143250ms") {
				t.Fatalf("error %q missing %q, segment or frame", err, tc.want)
			}
			if len(tc.runner.calls) != tc.calls {
				t.Fatalf("calls after failure: %+v", tc.runner.calls)
			}
			assertEmptyDir(t, tmp)
		})
	}
}

func TestAnalyzeCancellation(t *testing.T) {
	t.Run("before start", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		runner := &fakeRunner{}
		a, _ := newAdapter(t, baseConfig(), runner)
		if _, err := a.Analyze(ctx, request(vision.SourceLocal, "/m.mp4")); !errors.Is(err, context.Canceled) {
			t.Fatalf("err: %v", err)
		}
		if len(runner.calls) != 0 {
			t.Fatal("command ran after cancellation")
		}
	})
	t.Run("during analysis", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		runner := &fakeRunner{hook: func(name string) {
			if name == "llama-mtmd-cli" {
				cancel()
			}
		}}
		a, tmp := newAdapter(t, baseConfig(), runner)
		_, err := a.Analyze(ctx, request(vision.SourceLocal, "/m.mp4"))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err: %v", err)
		}
		if len(runner.calls) != 2 {
			t.Fatalf("calls after cancellation: %+v", runner.calls)
		}
		assertEmptyDir(t, tmp)
	})
}

func TestNewFromOptions(t *testing.T) {
	analyzer, err := NewFromOptions(vision.Options{
		"model": "/m/vlm.gguf", "mmproj": "/m/mmproj.gguf", "binary": "/b/llama-mtmd-cli", "ffmpeg": "/b/ffmpeg",
		"threads": "8", "gpu-layers": "0", "max-tokens": "300", "max-edge": "640",
	})
	if err != nil {
		t.Fatal(err)
	}
	a := analyzer.(*Adapter)
	want := Config{Model: "/m/vlm.gguf", MMProj: "/m/mmproj.gguf", Binary: "/b/llama-mtmd-cli", FFmpeg: "/b/ffmpeg",
		Threads: 8, GPULayers: ptr(0), MaxTokens: 300, MaxEdge: 640}
	if !reflect.DeepEqual(a.cfg, want) {
		t.Fatalf("config: %+v", a.cfg)
	}
	if _, ok := a.runner.(ExecRunner); !ok {
		t.Fatalf("runner: %T", a.runner)
	}
	if a.Model() != "vlm.gguf" {
		t.Fatalf("model label: %q", a.Model())
	}

	for name, tc := range map[string]struct {
		opts vision.Options
		want string
	}{
		"missing model":    {vision.Options{"mmproj": "p"}, `option "model"`},
		"missing mmproj":   {vision.Options{"model": "m"}, `option "mmproj"`},
		"blank model":      {vision.Options{"model": " ", "mmproj": "p"}, `option "model"`},
		"unknown option":   {vision.Options{"model": "m", "mmproj": "p", "modle": "x"}, `unknown option "modle"`},
		"download flag":    {vision.Options{"model": "m", "mmproj": "p", "hf-repo": "x"}, `unknown option "hf-repo"`},
		"bad threads":      {vision.Options{"model": "m", "mmproj": "p", "threads": "many"}, `option "threads"`},
		"zero threads":     {vision.Options{"model": "m", "mmproj": "p", "threads": "0"}, `option "threads"`},
		"negative layers":  {vision.Options{"model": "m", "mmproj": "p", "gpu-layers": "-1"}, `option "gpu-layers"`},
		"zero max tokens":  {vision.Options{"model": "m", "mmproj": "p", "max-tokens": "0"}, `option "max-tokens"`},
		"bad max edge":     {vision.Options{"model": "m", "mmproj": "p", "max-edge": "1.5"}, `option "max-edge"`},
		"negative in code": {nil, "must not be negative"},
	} {
		t.Run(name, func(t *testing.T) {
			var err error
			if tc.opts == nil {
				cfg := baseConfig()
				cfg.Threads = -1
				_, err = New(cfg)
			} else {
				_, err = NewFromOptions(tc.opts)
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v does not contain %q", err, tc.want)
			}
		})
	}
}
