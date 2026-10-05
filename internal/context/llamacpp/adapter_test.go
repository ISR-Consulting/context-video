package llamacpp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	contextcore "github.com/ISR-Consulting/context-video/internal/context"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

var segment = contracts.MediaSegment{
	SegmentID: "vod-cooking-001:10000-15000",
	Content:   contracts.ContentRef{ContentID: "vod-cooking-001", ContentType: contracts.ContentTypeVOD},
	Window:    contracts.TimeWindow{StartMs: 10000, EndMs: 15000},
}

func ptr[T any](v T) *T { return &v }

func testGroup(t *testing.T) contextcore.EvidenceGroup {
	t.Helper()
	description := "hand pouring oil into a pan"
	groups, err := contextcore.SegmentCorrelator{}.Correlate(contextcore.CorrelationInput{
		Segment: segment,
		Audio: []contracts.AudioObservation{{
			ObservationID: "aud:" + segment.SegmentID, Content: segment.Content, Window: segment.Window,
			Transcript: contracts.Transcript{Text: "agora vou colocar o azeite", Language: ptr("pt")},
			Provenance: contracts.ObservationProvenance{Provider: "fake", PipelineVersion: "poc-v1"},
		}},
		Visual: []contracts.VisualObservation{{
			ObservationID: "vis:" + segment.SegmentID, Content: segment.Content, Window: segment.Window,
			Frames: []contracts.VisualFrame{
				{TimestampMs: 11250, Description: &description, Observations: []contracts.VisualDetection{
					{Type: contracts.VisualDetectionObject, Value: "olive_oil_bottle", Confidence: 0.87},
				}},
				{TimestampMs: 13750, Observations: []contracts.VisualDetection{
					{Type: contracts.VisualDetectionAction, Value: "pouring", Confidence: 0.84},
				}},
			},
			Provenance: contracts.ObservationProvenance{Provider: "fake", PipelineVersion: "poc-v1"},
		}},
	})
	if err != nil || len(groups) != 1 {
		t.Fatalf("correlate: %v %d", err, len(groups))
	}
	return groups[0]
}

// fakeRunner records invocations, captures the scratch files named in argv
// and returns scripted output.
type fakeRunner struct {
	calls  [][]string
	files  map[string]string
	stdout []byte
	stderr []byte
	err    error
	hook   func(ctx context.Context)
}

func (f *fakeRunner) Run(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	f.files = map[string]string{}
	for i, a := range args {
		switch a {
		case "-sysf", "-f", "--json-schema-file":
			data, err := os.ReadFile(args[i+1])
			if err != nil {
				return nil, nil, err
			}
			f.files[a] = string(data)
		}
	}
	if f.hook != nil {
		f.hook(ctx)
	}
	return f.stdout, f.stderr, f.err
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func newAdapter(t *testing.T, r CommandRunner, cfg Config) (*Adapter, string) {
	t.Helper()
	tmp := t.TempDir()
	if cfg.Model == "" {
		cfg.Model = "/models/qwen2.5-7b-instruct-q4_k_m.gguf"
	}
	a, err := New(cfg, WithRunner(r), WithTempDir(tmp))
	if err != nil {
		t.Fatal(err)
	}
	return a, tmp
}

func TestReasonInvocationAndParsing(t *testing.T) {
	r := &fakeRunner{stdout: fixture(t, "cooking.txt")}
	a, tmp := newAdapter(t, r, Config{Threads: 4, GPULayers: ptr(0), MaxTokens: 256, CtxSize: 4096, Binary: "/opt/llama/llama-completion"})
	group := testGroup(t)
	candidates, err := a.Reason(context.Background(), group)
	if err != nil {
		t.Fatal(err)
	}

	if len(r.calls) != 1 {
		t.Fatalf("calls: %v", r.calls)
	}
	argv := r.calls[0]
	dir := filepath.Dir(argv[6])
	want := []string{
		"/opt/llama/llama-completion",
		"-m", "/models/qwen2.5-7b-instruct-q4_k_m.gguf",
		"-cnv", "-st",
		"-sysf", filepath.Join(dir, "system.txt"),
		"-f", filepath.Join(dir, "user.txt"),
		"--json-schema-file", filepath.Join(dir, "schema.json"),
		"--temp", "0", "--seed", "0", "-n", "256",
		"--no-display-prompt", "--simple-io", "--no-warmup", "--offline",
		"-t", "4", "-ngl", "0", "-c", "4096",
	}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("argv:\n%q\nwant\n%q", argv, want)
	}
	if filepath.Dir(dir) != tmp {
		t.Fatalf("scratch dir %s not under %s", dir, tmp)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scratch dir not removed: %v", err)
	}
	for _, arg := range argv {
		if strings.HasPrefix(arg, "-hf") || strings.Contains(arg, "http") {
			t.Fatalf("network flag %q", arg)
		}
	}

	if r.files["-sysf"] != SystemPrompt {
		t.Fatal("system prompt file differs from SystemPrompt")
	}
	for _, rule := range []string{
		"own judgement", "do not give every item the same value", "uncalibrated model outputs, not probabilities", "Never propose products, prices, retailers, offers",
		"PERSON, SPORTS_TEAM, ORGANIZATION, PLACE, EVENT", "lowercase snake_case", "cite both", "must cite that frame",
	} {
		if !strings.Contains(SystemPrompt, rule) {
			t.Fatalf("prompt lacks %q", rule)
		}
	}
	// Sources carry no observation IDs and no perception confidences.
	wantUser := `Sources:
{"window":{"startMs":10000,"endMs":15000},"sources":[` +
		`{"source":"a1","kind":"AUDIO","transcript":"agora vou colocar o azeite","language":"pt"},` +
		`{"source":"v1","kind":"VISUAL","timestampMs":11250,"description":"hand pouring oil into a pan","detections":[{"type":"OBJECT","value":"olive_oil_bottle"}]},` +
		`{"source":"v2","kind":"VISUAL","timestampMs":13750,"detections":[{"type":"ACTION","value":"pouring"}]}]}`
	if r.files["-f"] != wantUser {
		t.Fatalf("user prompt:\n%s\nwant\n%s", r.files["-f"], wantUser)
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(r.files["--json-schema-file"]), &schema); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"enum":["a1","v1","v2"]`, `"enum":["PERSON","SPORTS_TEAM","ORGANIZATION","PLACE","EVENT"]`, `"pattern":"^[a-z0-9]+(_[a-z0-9]+)*$"`,
	} {
		if !strings.Contains(r.files["--json-schema-file"], want) {
			t.Fatalf("schema lacks %s: %s", want, r.files["--json-schema-file"])
		}
	}

	if len(candidates) != 1 {
		t.Fatalf("candidates: %+v", candidates)
	}
	c := candidates[0]
	if c.Reasoning != (contextcore.ReasoningMetadata{Provider: ProviderName, Model: "qwen2.5-7b-instruct-q4_k_m.gguf", PromptVersion: PromptVersion}) {
		t.Fatalf("reasoning: %+v", c.Reasoning)
	}
	// Evidence is the union of the items' sources (a1, v1) in source order.
	if *c.Confidence != 0.8 || c.Topics[0].Value != "cooking" || c.Objects[0].Value != "olive_oil" || len(c.Brands) != 0 ||
		c.Entities[0].Type != "PERSON" || *c.Entities[0].Confidence != 0.4 || len(c.Evidence) != 2 ||
		c.Evidence[0].ObservationID != "aud:vod-cooking-001:10000-15000" || c.Evidence[0].TimestampMs != nil ||
		c.Evidence[1].ObservationID != "vis:vod-cooking-001:10000-15000" || *c.Evidence[1].TimestampMs != 11250 {
		t.Fatalf("candidate: %+v", c)
	}
	if err := contextcore.Ground(group, c); err != nil {
		t.Fatalf("parsed candidate does not ground: %v", err)
	}
}

func TestResponseSchemaSingleModality(t *testing.T) {
	g := testGroup(t)
	var visualOnly contextcore.EvidenceGroup = g
	visualOnly.Evidence = g.Evidence[1:]
	data, err := ResponseSchema(visualOnly)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"a1"`) || !strings.Contains(string(data), `"enum":["v1","v2"]`) {
		t.Fatalf("schema: %s", data)
	}
	again, _ := ResponseSchema(visualOnly)
	if string(again) != string(data) {
		t.Fatal("schema is not deterministic")
	}
}

func TestReasonParseFailures(t *testing.T) {
	cases := map[string]struct {
		stdout string
		want   string
	}{
		"empty":             {"  [end of text]\n", "empty model answer"},
		"not json":          {"I think it is cooking.", "decode model answer"},
		"trailing data":     {`{"events":[]} extra`, "unexpected data"},
		"two objects":       {`{"events":[]}{"events":[]}`, "unexpected data"},
		"unknown field":     {`{"events":[],"note":"x"}`, "unknown field"},
		"no events":         {`{}`, `no "events" array`},
		"missing topics":    {`{"events":[{"entities":[],"objects":[],"brands":[],"confidence":0.5}]}`, `missing "topics"`},
		"no items":          {`{"events":[{"entities":[],"topics":[],"objects":[],"brands":[],"confidence":0.5}]}`, "no entities, topics, objects or brands"},
		"missing conf":      {`{"events":[{"entities":[],"topics":[{"value":"x","sources":["a1"]}],"objects":[],"brands":[],"confidence":0.5}]}`, "topics[0].confidence: missing"},
		"event conf high":   {`{"events":[{"entities":[],"topics":[{"value":"x","confidence":1,"sources":["a1"]}],"objects":[],"brands":[],"confidence":1.5}]}`, "outside [0, 1]"},
		"entity field":      {`{"events":[{"entities":[{"type":"PERSON","value":"y","confidence":1,"sources":["a1"],"price":3}],"topics":[],"objects":[],"brands":[],"confidence":1}]}`, "unknown field"},
		"v1 evidence field": {`{"events":[{"entities":[],"topics":[{"value":"x","confidence":1,"sources":["a1"]}],"objects":[],"brands":[],"confidence":1,"evidence":[]}]}`, `unknown field "evidence"`},
		"open entity type":  {`{"events":[{"entities":[{"type":"ACTIVITY","value":"cooking","confidence":1,"sources":["a1"]}],"topics":[],"objects":[],"brands":[],"confidence":1}]}`, `type "ACTIVITY" is not one of`},
		"label with space":  {`{"events":[{"entities":[],"topics":[{"value":"soccer match","confidence":1,"sources":["a1"]}],"objects":[],"brands":[],"confidence":1}]}`, "not a lowercase snake_case label"},
		"label not english": {`{"events":[{"entities":[],"topics":[],"objects":[],"brands":[{"value":"Sérvia","confidence":1,"sources":["a1"]}],"confidence":1}]}`, "not a lowercase snake_case label"},
		"no sources":        {`{"events":[{"entities":[],"topics":[{"value":"x","confidence":1,"sources":[]}],"objects":[],"brands":[],"confidence":1}]}`, "topics[0]: no sources"},
		"missing sources":   {`{"events":[{"entities":[],"topics":[{"value":"x","confidence":1}],"objects":[],"brands":[],"confidence":1}]}`, "topics[0]: no sources"},
		"unknown source":    {`{"events":[{"entities":[],"topics":[{"value":"x","confidence":1,"sources":["v9"]}],"objects":[],"brands":[],"confidence":1}]}`, `unknown source "v9"`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			a, _ := newAdapter(t, &fakeRunner{stdout: []byte(tc.stdout)}, Config{})
			_, err := a.Reason(context.Background(), testGroup(t))
			var ae *Error
			if !errors.As(err, &ae) || ae.Step != StepParse || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want parse error containing %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), "segment "+segment.SegmentID) {
				t.Fatalf("error lacks segment: %v", err)
			}
		})
	}
}

func TestReasonAcceptedAnswerForms(t *testing.T) {
	for name, stdout := range map[string][]byte{
		"fenced":        fixture(t, "fenced.txt"),
		"no candidates": []byte("{\"events\": []} [end of text]\n\n"),
	} {
		t.Run(name, func(t *testing.T) {
			a, _ := newAdapter(t, &fakeRunner{stdout: stdout}, Config{})
			candidates, err := a.Reason(context.Background(), testGroup(t))
			if err != nil {
				t.Fatal(err)
			}
			if name == "no candidates" && len(candidates) != 0 {
				t.Fatalf("%+v", candidates)
			}
			if name == "fenced" && len(candidates) != 1 {
				t.Fatalf("%+v", candidates)
			}
		})
	}
}

func TestReasonCandidatesCiteFramesOfVisualLabels(t *testing.T) {
	// The brand comes from frame v2 and the topic from the transcript: the
	// candidate's evidence must contain both, and the core grounds it.
	stdout := `{"events":[{"entities":[],"topics":[{"value":"cooking","confidence":0.7,"sources":["a1"]}],"objects":[],` +
		`"brands":[{"value":"gallo","confidence":0.5,"sources":["v2","v2"]}],"confidence":0.6}]}`
	a, _ := newAdapter(t, &fakeRunner{stdout: []byte(stdout)}, Config{})
	group := testGroup(t)
	candidates, err := a.Reason(context.Background(), group)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("%v %v", candidates, err)
	}
	ev := candidates[0].Evidence
	if len(ev) != 2 || ev[0].TimestampMs != nil || *ev[1].TimestampMs != 13750 {
		t.Fatalf("evidence %+v", ev)
	}
	if err := contextcore.Ground(group, candidates[0]); err != nil {
		t.Fatalf("grounding: %v", err)
	}
}

func TestReasonCommandFailure(t *testing.T) {
	r := &fakeRunner{stderr: []byte("error: failed to load model\n"), err: errors.New("exit status 1")}
	a, _ := newAdapter(t, r, Config{})
	_, err := a.Reason(context.Background(), testGroup(t))
	var ae *Error
	if !errors.As(err, &ae) || ae.Step != StepReason || !strings.Contains(err.Error(), "failed to load model") || !strings.Contains(err.Error(), "llama-completion") {
		t.Fatalf("%v", err)
	}
}

func TestReasonCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &fakeRunner{}
	a, _ := newAdapter(t, r, Config{})
	if _, err := a.Reason(ctx, testGroup(t)); !errors.Is(err, context.Canceled) || len(r.calls) != 0 {
		t.Fatalf("pre-cancelled: %v calls %d", err, len(r.calls))
	}

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	r = &fakeRunner{err: errors.New("signal: killed"), hook: func(context.Context) { cancel() }}
	a, _ = newAdapter(t, r, Config{})
	if _, err := a.Reason(ctx, testGroup(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("in-flight: %v", err)
	}
}

func TestReasonEmptyGroupRunsNothing(t *testing.T) {
	r := &fakeRunner{}
	a, _ := newAdapter(t, r, Config{})
	candidates, err := a.Reason(context.Background(), contextcore.EvidenceGroup{SegmentID: "x"})
	if err != nil || candidates != nil || len(r.calls) != 0 {
		t.Fatalf("%v %v %d", candidates, err, len(r.calls))
	}
}

func TestDefaultsAndOptions(t *testing.T) {
	r := &fakeRunner{stdout: []byte(`{"events":[]}`)}
	a, _ := newAdapter(t, r, Config{})
	if _, err := a.Reason(context.Background(), testGroup(t)); err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(r.calls[0], " ")
	if !strings.HasPrefix(argv, "llama-completion ") || !strings.Contains(argv, "-n 1024") || strings.Contains(argv, " -t ") ||
		strings.Contains(argv, " -ngl ") || strings.Contains(argv, " -c ") {
		t.Fatalf("defaults: %s", argv)
	}

	reasoner, err := NewFromOptions(contextcore.Options{"model": "/m/x.gguf", "binary": "b", "threads": "2", "gpu-layers": "0", "max-tokens": "64", "ctx-size": "2048"})
	if err != nil {
		t.Fatal(err)
	}
	got := reasoner.(*Adapter).cfg
	if got.Model != "/m/x.gguf" || got.Binary != "b" || got.Threads != 2 || *got.GPULayers != 0 || got.MaxTokens != 64 || got.CtxSize != 2048 {
		t.Fatalf("%+v", got)
	}
	for name, tc := range map[string]struct {
		opts contextcore.Options
		want string
	}{
		"missing model": {contextcore.Options{}, `option "model"`},
		"unknown key":   {contextcore.Options{"model": "m", "temperature": "1"}, `unknown option "temperature"`},
		"bad threads":   {contextcore.Options{"model": "m", "threads": "0"}, `"threads" must be a positive integer`},
		"bad layers":    {contextcore.Options{"model": "m", "gpu-layers": "-1"}, `"gpu-layers" must be a non-negative integer`},
		"bad tokens":    {contextcore.Options{"model": "m", "max-tokens": "x"}, `"max-tokens"`},
		"bad ctx":       {contextcore.Options{"model": "m", "ctx-size": "-5"}, `"ctx-size"`},
	} {
		if _, err := NewFromOptions(tc.opts); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := New(Config{Model: "m", Threads: -1}); err == nil {
		t.Fatal("negative threads accepted")
	}
}

func TestErrorFormatting(t *testing.T) {
	err := &Error{Step: StepParse, SegmentID: "s", Output: "junk", Stderr: "tail", Err: errors.New("bad")}
	if err.Error() != "llama-cpp parse: segment s: bad (output: junk) (stderr: tail)" {
		t.Fatal(err.Error())
	}
	var nilErr *Error
	if nilErr.Error() != "" || nilErr.Unwrap() != nil {
		t.Fatal("nil error")
	}
	long := strings.Repeat("x", maxExcerpt+10)
	if got := excerpt([]byte(long)); len(got) != maxExcerpt+len("…") {
		t.Fatalf("excerpt length %d", len(got))
	}
}
