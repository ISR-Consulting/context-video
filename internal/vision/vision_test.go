package vision_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ISR-Consulting/context-video/internal/vision"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

func segment() contracts.MediaSegment {
	uri := "media/football.mp4"
	return contracts.MediaSegment{
		SegmentID: "live-xyz:142000-147000",
		Content:   contracts.ContentRef{ContentID: "live-xyz", ContentType: contracts.ContentTypeLive},
		Window:    contracts.TimeWindow{StartMs: 142000, EndMs: 147000},
		SourceURI: &uri,
	}
}

func ptr[T any](v T) *T { return &v }

func stub(name string) vision.Factory {
	return func(vision.Options) (vision.Analyzer, error) {
		return vision.AnalyzerFunc(func(context.Context, vision.Request) (vision.Analysis, error) {
			return vision.Analysis{Provider: name}, nil
		}), nil
	}
}

func TestRegistry(t *testing.T) {
	r := vision.NewRegistry()
	for _, name := range []string{"zeta", "alpha"} {
		if err := r.Register(name, stub(name)); err != nil {
			t.Fatal(err)
		}
	}
	if got := r.Names(); !reflect.DeepEqual(got, []string{"alpha", "zeta"}) {
		t.Fatalf("names: %v", got)
	}
	for _, bad := range []struct {
		name    string
		factory vision.Factory
	}{{"", stub("x")}, {"  ", stub("x")}, {"new", nil}, {"alpha", stub("alpha")}} {
		if err := r.Register(bad.name, bad.factory); err == nil {
			t.Errorf("Register(%q) accepted", bad.name)
		}
	}
	analyzer, err := r.New("alpha", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := analyzer.Analyze(context.Background(), vision.Request{}); err != nil || got.Provider != "alpha" {
		t.Fatalf("analyzer: %+v %v", got, err)
	}
	_, err = r.New("TBD", nil)
	if !errors.Is(err, vision.ErrUnknownProvider) {
		t.Fatalf("unknown provider: %v", err)
	}
	if want := `unknown vision provider "TBD"; available: alpha, zeta`; err.Error() != want {
		t.Fatalf("error %q want %q", err, want)
	}
	if _, err := vision.NewRegistry().New("x", nil); err == nil || !strings.Contains(err.Error(), "available: none") {
		t.Fatalf("empty registry: %v", err)
	}
}

func TestRegistryFactoryFailures(t *testing.T) {
	r := vision.NewRegistry()
	cause := errors.New("model missing")
	_ = r.Register("broken", func(vision.Options) (vision.Analyzer, error) { return nil, cause })
	_ = r.Register("nil", func(vision.Options) (vision.Analyzer, error) { return nil, nil })
	var seen vision.Options
	_ = r.Register("opts", func(o vision.Options) (vision.Analyzer, error) { seen = o; return stub("opts")(o) })

	if _, err := r.New("broken", nil); !errors.Is(err, cause) || !strings.Contains(err.Error(), `vision provider "broken"`) {
		t.Fatalf("broken: %v", err)
	}
	if _, err := r.New("nil", nil); err == nil {
		t.Fatal("nil analyzer accepted")
	}
	if _, err := r.New("opts", vision.Options{"model": "m.gguf"}); err != nil || seen["model"] != "m.gguf" {
		t.Fatalf("options not forwarded: %v %v", seen, err)
	}
}

func TestParseSampling(t *testing.T) {
	for raw, want := range map[string]int{"uniform:1": 1, "uniform:3": 3, " uniform:16 ": 16} {
		s, err := vision.ParseSampling(raw)
		if err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		if s != (vision.Uniform{Frames: want}) {
			t.Fatalf("%q: %+v", raw, s)
		}
	}
	for _, raw := range []string{"TBD", "", "uniform", "uniform:", "uniform:0", "uniform:17", "uniform:-1", "uniform:x", "scene:0.3", "Uniform:2"} {
		_, err := vision.ParseSampling(raw)
		if !errors.Is(err, vision.ErrUnsupportedSampling) {
			t.Errorf("%q: %v", raw, err)
		}
	}
	_, err := vision.ParseSampling("TBD")
	if want := `unsupported vision sampling policy "TBD"; supported: uniform:N (N = 1..16)`; err.Error() != want {
		t.Fatalf("error %q want %q", err, want)
	}
	if got := (vision.Uniform{Frames: 2}).String(); got != "uniform:2" {
		t.Fatalf("string: %q", got)
	}
}

func TestUniformFrameTimes(t *testing.T) {
	cases := []struct {
		frames     int
		start, end int64
		want       []int64
	}{
		{1, 0, 5000, []int64{2500}},
		{2, 0, 5000, []int64{1250, 3750}},
		{2, 142000, 147000, []int64{143250, 145750}},
		{3, 0, 2000, []int64{333, 1000, 1666}},
		{4, 0, 10000, []int64{1250, 3750, 6250, 8750}},
		{2, 5000, 8000, []int64{5750, 7250}},
		{1, 9999, 10000, []int64{9999}},
		{3, 9999, 10000, []int64{9999}},
		{4, 0, 3, []int64{0, 1, 2}},
	}
	for _, tc := range cases {
		window := contracts.TimeWindow{StartMs: tc.start, EndMs: tc.end}
		got, err := vision.Uniform{Frames: tc.frames}.FrameTimes(window)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("uniform:%d [%d,%d): %v want %v", tc.frames, tc.start, tc.end, got, tc.want)
		}
		for i, ts := range got {
			if ts < tc.start || ts >= tc.end || (i > 0 && ts <= got[i-1]) {
				t.Errorf("timestamp %d not ascending inside [%d,%d)", ts, tc.start, tc.end)
			}
		}
	}
	for _, bad := range []struct {
		u vision.Uniform
		w contracts.TimeWindow
	}{
		{vision.Uniform{Frames: 0}, contracts.TimeWindow{EndMs: 10}},
		{vision.Uniform{Frames: 17}, contracts.TimeWindow{EndMs: 10}},
		{vision.Uniform{Frames: 1}, contracts.TimeWindow{StartMs: 5, EndMs: 5}},
		{vision.Uniform{Frames: 1}, contracts.TimeWindow{StartMs: -1, EndMs: 5}},
	} {
		if _, err := bad.u.FrameTimes(bad.w); err == nil {
			t.Errorf("%+v %+v accepted", bad.u, bad.w)
		}
	}
}

func TestObservationIDIsDeterministic(t *testing.T) {
	if got := vision.ObservationID(segment()); got != "vis:live-xyz:142000-147000" {
		t.Fatalf("id: %q", got)
	}
}

func analysis() vision.Analysis {
	return vision.Analysis{
		Frames: []vision.Frame{
			{
				TimestampMs: 143250,
				Description: "  Palmeiras jersey on a player  ",
				Detections: []vision.Detection{
					{Type: contracts.VisualDetectionObject, Value: " football_jersey ", Confidence: ptr(0.91)},
					{Type: contracts.VisualDetectionEntity, Value: "Palmeiras", Confidence: ptr(0.0)},
				},
			},
			{TimestampMs: 145750},
		},
		Provider: "fake",
		Model:    "fake-vlm",
	}
}

func TestNewObservationMapsAllFields(t *testing.T) {
	times := []int64{143250, 145750}
	obs, err := vision.NewObservation(segment(), times, analysis(), "poc-v1")
	if err != nil {
		t.Fatal(err)
	}
	want := contracts.VisualObservation{
		ObservationID: "vis:live-xyz:142000-147000",
		Content:       segment().Content,
		Window:        segment().Window,
		Frames: []contracts.VisualFrame{
			{
				TimestampMs: 143250,
				Description: ptr("Palmeiras jersey on a player"),
				Observations: []contracts.VisualDetection{
					{Type: contracts.VisualDetectionObject, Value: "football_jersey", Confidence: 0.91},
					{Type: contracts.VisualDetectionEntity, Value: "Palmeiras", Confidence: 0},
				},
			},
			{TimestampMs: 145750, Observations: []contracts.VisualDetection{}},
		},
		Provenance: contracts.ObservationProvenance{Provider: "fake", Model: ptr("fake-vlm"), PipelineVersion: "poc-v1"},
	}
	if !reflect.DeepEqual(obs, want) {
		t.Fatalf("observation:\n%+v\nwant:\n%+v", obs, want)
	}
	data := assertSchemaValid(t, obs)
	if !strings.Contains(string(data), `{"timestampMs":145750,"observations":[]}`) {
		t.Fatalf("empty frame not serialized as empty observations: %s", data)
	}
	if strings.Contains(string(data), `"model":""`) {
		t.Fatal("blank model serialized")
	}
}

func TestNewObservationOmitsBlankModel(t *testing.T) {
	a := analysis()
	a.Model = ""
	obs, err := vision.NewObservation(segment(), []int64{143250, 145750}, a, "poc-v1")
	if err != nil || obs.Provenance.Model != nil {
		t.Fatalf("model: %v %v", obs.Provenance.Model, err)
	}
}

func TestNewObservationRejectsInvalidInput(t *testing.T) {
	times := []int64{143250, 145750}
	with := func(mutate func(*vision.Analysis)) vision.Analysis {
		a := analysis()
		a.Frames = append([]vision.Frame(nil), a.Frames...)
		a.Frames[0].Detections = append([]vision.Detection(nil), a.Frames[0].Detections...)
		mutate(&a)
		return a
	}
	cases := map[string]struct {
		segment contracts.MediaSegment
		times   []int64
		a       vision.Analysis
		version string
		want    string
	}{
		"blank provider":   {segment(), times, with(func(a *vision.Analysis) { a.Provider = " " }), "poc-v1", "provider"},
		"blank version":    {segment(), times, analysis(), "", "pipeline version"},
		"no frames":        {segment(), nil, vision.Analysis{Provider: "fake"}, "poc-v1", "at least one sampled frame"},
		"missing frame":    {segment(), times, with(func(a *vision.Analysis) { a.Frames = a.Frames[:1] }), "poc-v1", "1 frames for 2"},
		"moved frame":      {segment(), times, with(func(a *vision.Analysis) { a.Frames[1].TimestampMs = 146000 }), "poc-v1", "does not match"},
		"unknown type":     {segment(), times, with(func(a *vision.Analysis) { a.Frames[0].Detections[0].Type = "PRODUCT" }), "poc-v1", `type "PRODUCT"`},
		"blank value":      {segment(), times, with(func(a *vision.Analysis) { a.Frames[0].Detections[0].Value = "  " }), "poc-v1", "value must not be blank"},
		"no confidence":    {segment(), times, with(func(a *vision.Analysis) { a.Frames[0].Detections[0].Confidence = nil }), "poc-v1", "provider supplied none"},
		"confidence > 1":   {segment(), times, with(func(a *vision.Analysis) { a.Frames[0].Detections[0].Confidence = ptr(1.01) }), "poc-v1", "within [0, 1]"},
		"confidence < 0":   {segment(), times, with(func(a *vision.Analysis) { a.Frames[0].Detections[0].Confidence = ptr(-0.1) }), "poc-v1", "within [0, 1]"},
		"NaN confidence":   {segment(), times, with(func(a *vision.Analysis) { a.Frames[0].Detections[0].Confidence = ptr(math.NaN()) }), "poc-v1", "within [0, 1]"},
		"frame off window": {segment(), []int64{143250, 150000}, with(func(a *vision.Analysis) { a.Frames[1].TimestampMs = 150000 }), "poc-v1", "frame timestamp must belong"},
		"blank segment id": {func() contracts.MediaSegment { s := segment(); s.SegmentID = ""; return s }(), times, analysis(), "poc-v1", "segment id"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := vision.NewObservation(tc.segment, tc.times, tc.a, tc.version)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v does not contain %q", err, tc.want)
			}
		})
	}
}

func TestDetectionTypesMatchContract(t *testing.T) {
	var schema struct {
		Properties struct {
			Frames struct {
				Items struct {
					Properties struct {
						Observations struct {
							Items struct {
								Properties struct {
									Type struct {
										Enum []contracts.VisualDetectionType `json:"enum"`
									} `json:"type"`
								} `json:"properties"`
							} `json:"items"`
						} `json:"observations"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"frames"`
		} `json:"properties"`
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "specs", "visual-observation.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	enum := schema.Properties.Frames.Items.Properties.Observations.Items.Properties.Type.Enum
	if !reflect.DeepEqual(vision.DetectionTypes(), enum) {
		t.Fatalf("types %v, schema enum %v", vision.DetectionTypes(), enum)
	}
}

func assertSchemaValid(t *testing.T, obs contracts.VisualObservation) []byte {
	t.Helper()
	validator, err := contracts.NewValidator(os.DirFS(filepath.Join("..", "..", "specs")))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(obs)
	if err != nil {
		t.Fatal(err)
	}
	if err := validator.ValidateJSON(contracts.KindVisualObservation, data); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return data
}
