package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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
			},
			Provenance: contracts.ObservationProvenance{Provider: "fake", PipelineVersion: "poc-v1"},
		}},
	})
	if err != nil || len(groups) != 1 {
		t.Fatalf("correlate: %v %d", err, len(groups))
	}
	return groups[0]
}

func newTestAdapter(t *testing.T, handler http.HandlerFunc, cfg Config) *Adapter {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	if cfg.APIKey == "" {
		cfg.APIKey = "test-key"
	}
	cfg.BaseURL = srv.URL
	a, err := New(cfg, WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestReasonMapsJSON(t *testing.T) {
	var gotBody map[string]any
	answer := `{"events":[{"entities":[],"topics":[{"value":"cooking","confidence":0.9,"sources":["a1","v1"]}],"objects":[{"value":"olive_oil_bottle","confidence":0.88,"sources":["v1"]}],"brands":[],"confidence":0.91}]}`
	handler := func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + mustQuote(answer) + `}}]}`))
	}
	a := newTestAdapter(t, handler, Config{})
	got, err := a.Reason(context.Background(), testGroup(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("%d candidates", len(got))
	}
	c := got[0]
	if c.Reasoning.Provider != ProviderName || c.Reasoning.Model != defaultModel || c.Reasoning.PromptVersion != PromptVersion {
		t.Fatalf("meta %+v", c.Reasoning)
	}
	if len(c.Topics) != 1 || c.Topics[0].Value != "cooking" {
		t.Fatalf("topics %+v", c.Topics)
	}
	if len(c.Evidence) < 2 {
		t.Fatalf("evidence %+v", c.Evidence)
	}
	msgs := gotBody["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages %v", msgs)
	}
	sys := msgs[0].(map[string]any)["content"].(string)
	if !strings.Contains(sys, "semantic reasoning") {
		t.Fatalf("system prompt missing")
	}
}

func mustQuote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestReasonEmptyEvidence(t *testing.T) {
	called := false
	a := newTestAdapter(t, func(http.ResponseWriter, *http.Request) { called = true }, Config{})
	got, err := a.Reason(context.Background(), contextcore.EvidenceGroup{SegmentID: "x"})
	if err != nil || got != nil || called {
		t.Fatalf("got %v err %v called %v", got, err, called)
	}
}

func TestReasonHTTPError(t *testing.T) {
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
	}, Config{})
	_, err := a.Reason(context.Background(), testGroup(t))
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("err %v", err)
	}
	var ae *Error
	if !errors.As(err, &ae) || ae.Step != StepReason {
		t.Fatalf("step %v", err)
	}
}

func TestReasonCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := newTestAdapter(t, func(http.ResponseWriter, *http.Request) {}, Config{})
	_, err := a.Reason(ctx, testGroup(t))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
}

func TestReasonParseError(t *testing.T) {
	a := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"events\":[{\"entities\":[],\"topics\":[],\"objects\":[],\"brands\":[],\"confidence\":0.5}]}"}}]}`))
	}, Config{})
	_, err := a.Reason(context.Background(), testGroup(t))
	var ae *Error
	if !errors.As(err, &ae) || ae.Step != StepParse {
		t.Fatalf("err %v", err)
	}
}

func TestNewFromOptions(t *testing.T) {
	t.Setenv(envAPIKey, "")
	_, err := NewFromOptions(contextcore.Options{})
	if err == nil || !strings.Contains(err.Error(), envAPIKey) {
		t.Fatalf("err %v", err)
	}
	got, err := NewFromOptions(contextcore.Options{OptionAPIKey: "k", OptionModel: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}
	if got.(*Adapter).cfg.Model != "gpt-4o-mini" {
		t.Fatal(got.(*Adapter).cfg.Model)
	}
}

func TestParseAnswerMapping(t *testing.T) {
	group := testGroup(t)
	srcs := sources(group)
	raw := []byte(`{"events":[{"entities":[{"type":"PLACE","value":"Kitchen","confidence":0.7,"sources":["v1"]}],"topics":[{"value":"cooking","confidence":0.9,"sources":["a1"]}],"objects":[],"brands":[],"confidence":0.8}]}`)
	got, err := parseAnswer(raw, contextcore.ReasoningMetadata{Provider: ProviderName, Model: "m", PromptVersion: PromptVersion}, srcs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !reflect.DeepEqual(got[0].Entities[0].Type, "PLACE") {
		t.Fatalf("%+v", got)
	}
}
