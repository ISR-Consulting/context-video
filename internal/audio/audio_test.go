package audio_test

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

	"github.com/ISR-Consulting/context-video/internal/audio"
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

func stub(name string) audio.Factory {
	return func(audio.Options) (audio.Transcriber, error) {
		return audio.TranscriberFunc(func(context.Context, audio.Request) (audio.Transcription, error) {
			return audio.Transcription{Provider: name}, nil
		}), nil
	}
}

func TestRegistry(t *testing.T) {
	r := audio.NewRegistry()
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
		factory audio.Factory
	}{{"", stub("x")}, {"  ", stub("x")}, {"new", nil}, {"alpha", stub("alpha")}} {
		if err := r.Register(bad.name, bad.factory); err == nil {
			t.Errorf("Register(%q) accepted", bad.name)
		}
	}

	transcriber, err := r.New("alpha", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := transcriber.Transcribe(context.Background(), audio.Request{})
	if err != nil || got.Provider != "alpha" {
		t.Fatalf("transcriber: %+v %v", got, err)
	}

	_, err = r.New("TBD", nil)
	if !errors.Is(err, audio.ErrUnknownProvider) {
		t.Fatalf("unknown provider: %v", err)
	}
	if want := `unknown audio provider "TBD"; available: alpha, zeta`; err.Error() != want {
		t.Fatalf("error %q want %q", err, want)
	}
	if _, err := audio.NewRegistry().New("x", nil); err == nil || !strings.Contains(err.Error(), "available: none") {
		t.Fatalf("empty registry: %v", err)
	}
}

func TestRegistryFactoryFailures(t *testing.T) {
	r := audio.NewRegistry()
	cause := errors.New("model missing")
	_ = r.Register("broken", func(audio.Options) (audio.Transcriber, error) { return nil, cause })
	_ = r.Register("nil", func(audio.Options) (audio.Transcriber, error) { return nil, nil })
	var seen audio.Options
	_ = r.Register("opts", func(o audio.Options) (audio.Transcriber, error) { seen = o; return stubTranscriber{}, nil })

	if _, err := r.New("broken", nil); !errors.Is(err, cause) || !strings.Contains(err.Error(), `audio provider "broken"`) {
		t.Fatalf("broken: %v", err)
	}
	if _, err := r.New("nil", nil); err == nil {
		t.Fatal("nil transcriber accepted")
	}
	if _, err := r.New("opts", audio.Options{"model": "m.bin"}); err != nil || seen["model"] != "m.bin" {
		t.Fatalf("options not forwarded: %v %v", seen, err)
	}
}

type stubTranscriber struct{}

func (stubTranscriber) Transcribe(context.Context, audio.Request) (audio.Transcription, error) {
	return audio.Transcription{}, nil
}

func TestObservationIDIsDeterministic(t *testing.T) {
	if got := audio.ObservationID(segment()); got != "aud:live-xyz:142000-147000" {
		t.Fatalf("id: %q", got)
	}
	if audio.ObservationID(segment()) != audio.ObservationID(segment()) {
		t.Fatal("not deterministic")
	}
}

func TestNewObservationMapsAllFields(t *testing.T) {
	obs, err := audio.NewObservation(segment(), audio.Transcription{
		Text:       "essa é a nova camisa do Palmeiras",
		Language:   "pt",
		Confidence: ptr(0.96),
		Provider:   "fake",
		Model:      "fake-model",
	}, "poc-v1")
	if err != nil {
		t.Fatal(err)
	}
	want := contracts.AudioObservation{
		ObservationID: "aud:live-xyz:142000-147000",
		Content:       segment().Content,
		Window:        segment().Window,
		Transcript:    contracts.Transcript{Text: "essa é a nova camisa do Palmeiras", Language: ptr("pt"), Confidence: ptr(0.96)},
		Provenance:    contracts.ObservationProvenance{Provider: "fake", Model: ptr("fake-model"), PipelineVersion: "poc-v1"},
	}
	if !reflect.DeepEqual(obs, want) {
		t.Fatalf("observation:\n%+v\nwant:\n%+v", obs, want)
	}
	assertSchemaValid(t, obs)
}

func TestNewObservationOmitsAbsentFields(t *testing.T) {
	obs, err := audio.NewObservation(segment(), audio.Transcription{Provider: "whisper-cpp"}, "poc-v1")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(obs)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"observationId":"aud:live-xyz:142000-147000","content":{"contentId":"live-xyz","contentType":"LIVE"},` +
		`"window":{"startMs":142000,"endMs":147000},"transcript":{"text":""},` +
		`"provenance":{"provider":"whisper-cpp","pipelineVersion":"poc-v1"}}`
	if string(data) != want {
		t.Fatalf("json:\n%s\nwant:\n%s", data, want)
	}
	assertSchemaValid(t, obs)
}

func TestNewObservationKeepsMeasuredZeroConfidence(t *testing.T) {
	obs, err := audio.NewObservation(segment(), audio.Transcription{Provider: "fake", Confidence: ptr(0.0)}, "poc-v1")
	if err != nil {
		t.Fatal(err)
	}
	if obs.Transcript.Confidence == nil || *obs.Transcript.Confidence != 0 {
		t.Fatalf("confidence: %v", obs.Transcript.Confidence)
	}
}

func TestNewObservationRejectsInvalidInput(t *testing.T) {
	cases := map[string]struct {
		segment contracts.MediaSegment
		t       audio.Transcription
		version string
	}{
		"blank provider":       {segment(), audio.Transcription{Provider: " "}, "poc-v1"},
		"blank version":        {segment(), audio.Transcription{Provider: "fake"}, ""},
		"confidence above one": {segment(), audio.Transcription{Provider: "fake", Confidence: ptr(1.01)}, "poc-v1"},
		"negative confidence":  {segment(), audio.Transcription{Provider: "fake", Confidence: ptr(-0.1)}, "poc-v1"},
		"NaN confidence":       {segment(), audio.Transcription{Provider: "fake", Confidence: ptr(math.NaN())}, "poc-v1"},
		"blank segment id": {func() contracts.MediaSegment {
			s := segment()
			s.SegmentID = ""
			return s
		}(), audio.Transcription{Provider: "fake"}, "poc-v1"},
		"invalid window": {func() contracts.MediaSegment {
			s := segment()
			s.Window.EndMs = s.Window.StartMs
			return s
		}(), audio.Transcription{Provider: "fake"}, "poc-v1"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := audio.NewObservation(tc.segment, tc.t, tc.version); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func assertSchemaValid(t *testing.T, obs contracts.AudioObservation) {
	t.Helper()
	validator, err := contracts.NewValidator(os.DirFS(filepath.Join("..", "..", "specs")))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(obs)
	if err != nil {
		t.Fatal(err)
	}
	if err := validator.ValidateJSON(contracts.KindAudioObservation, data); err != nil {
		t.Fatalf("schema: %v", err)
	}
}
