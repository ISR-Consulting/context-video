package context_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	contextcore "github.com/ISR-Consulting/context-video/internal/context"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

var (
	content = contracts.ContentRef{ContentID: "vod-cooking-001", ContentType: contracts.ContentTypeVOD}
	segment = contracts.MediaSegment{
		SegmentID: "vod-cooking-001:10000-15000",
		Content:   content,
		Window:    contracts.TimeWindow{StartMs: 10000, EndMs: 15000},
	}
	audioID  = "aud:" + segment.SegmentID
	visualID = "vis:" + segment.SegmentID
)

func ptr[T any](v T) *T { return &v }

func audioObs(text string) contracts.AudioObservation {
	return contracts.AudioObservation{
		ObservationID: audioID,
		Content:       content,
		Window:        segment.Window,
		Transcript:    contracts.Transcript{Text: text, Language: ptr("pt"), Confidence: ptr(0.7)},
		Provenance:    contracts.ObservationProvenance{Provider: "fake-stt", PipelineVersion: "poc-v1"},
	}
}

func visualObs(frames ...contracts.VisualFrame) contracts.VisualObservation {
	return contracts.VisualObservation{
		ObservationID: visualID,
		Content:       content,
		Window:        segment.Window,
		Frames:        frames,
		Provenance:    contracts.ObservationProvenance{Provider: "fake-vlm", PipelineVersion: "poc-v1"},
	}
}

func frame(ts int64, description string, detections ...contracts.VisualDetection) contracts.VisualFrame {
	f := contracts.VisualFrame{TimestampMs: ts, Observations: detections}
	if description != "" {
		f.Description = &description
	}
	if f.Observations == nil {
		f.Observations = []contracts.VisualDetection{}
	}
	return f
}

func det(t contracts.VisualDetectionType, value string, confidence float64) contracts.VisualDetection {
	return contracts.VisualDetection{Type: t, Value: value, Confidence: confidence}
}

// multimodalInput is one segment with a transcript and two frames: the first
// has a description and two detections, the second only one detection.
func multimodalInput() contextcore.CorrelationInput {
	return contextcore.CorrelationInput{
		Segment: segment,
		Audio:   []contracts.AudioObservation{audioObs("agora vou colocar o azeite [BLANK_AUDIO]")},
		Visual: []contracts.VisualObservation{visualObs(
			frame(11250, "hand pouring oil into a pan",
				det(contracts.VisualDetectionObject, "olive_oil_bottle", 0.87),
				det(contracts.VisualDetectionAction, "pouring", 0.84)),
			frame(13750, "", det(contracts.VisualDetectionObject, "frying_pan", 0.9)),
		)},
	}
}

func correlate(t *testing.T, in contextcore.CorrelationInput) contextcore.EvidenceGroup {
	t.Helper()
	groups, err := contextcore.SegmentCorrelator{}.Correlate(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(groups))
	}
	return groups[0]
}

func audioRef() contextcore.EvidenceRef { return contextcore.EvidenceRef{ObservationID: audioID} }

func visualRef(ts int64) contextcore.EvidenceRef {
	return contextcore.EvidenceRef{ObservationID: visualID, TimestampMs: ptr(ts)}
}

func candidate(refs ...contextcore.EvidenceRef) contextcore.Candidate {
	return contextcore.Candidate{
		Entities:   []contextcore.Entity{{Type: "ACTIVITY", Value: "cooking", Confidence: ptr(0.8)}},
		Topics:     []contextcore.Value{{Value: "cooking", Confidence: ptr(0.9)}},
		Objects:    []contextcore.Value{{Value: "olive_oil", Confidence: ptr(0.7)}},
		Brands:     []contextcore.Value{},
		Confidence: ptr(0.75),
		Evidence:   refs,
		Reasoning:  contextcore.ReasoningMetadata{Provider: "fake", Model: "fake-model.gguf", PromptVersion: "test-v1"},
	}
}

func TestProjectAudio(t *testing.T) {
	obs := audioObs("  gol [BLANK_AUDIO] ")
	got := contextcore.ProjectAudio(obs)
	if len(got) != 1 {
		t.Fatalf("got %d items", len(got))
	}
	e := got[0]
	if e.Kind != contextcore.EvidenceAudio || e.Facet != contextcore.FacetTranscript || e.ObservationID != audioID ||
		e.Text != "  gol [BLANK_AUDIO] " || e.Language != "pt" || e.TimestampMs != nil || e.Window != segment.Window {
		t.Fatalf("projection: %+v", e)
	}
	if e.Confidence == nil || *e.Confidence != 0.7 || e.Confidence == obs.Transcript.Confidence {
		t.Fatalf("confidence must be a copy of 0.7, got %v", e.Confidence)
	}
	noConfidence := audioObs("x")
	noConfidence.Transcript.Confidence, noConfidence.Transcript.Language = nil, nil
	if e := contextcore.ProjectAudio(noConfidence)[0]; e.Confidence != nil || e.Language != "" {
		t.Fatalf("absent values must stay absent: %+v", e)
	}
	for _, blank := range []string{"", "  \n\t"} {
		if got := contextcore.ProjectAudio(audioObs(blank)); got != nil {
			t.Fatalf("blank transcript %q projected: %+v", blank, got)
		}
	}
}

func TestProjectVisual(t *testing.T) {
	obs := visualObs(
		frame(11000, "a kitchen", det(contracts.VisualDetectionScene, "kitchen", 0.6), det(contracts.VisualDetectionObject, "pan", 0.5)),
		frame(12000, ""),
		frame(13000, "", det(contracts.VisualDetectionBrand, "Acme", 0.4)),
	)
	got := contextcore.ProjectVisual(obs)
	type row struct {
		facet contextcore.Facet
		ts    int64
		item  int
		text  string
		typ   string
		value string
		conf  float64
	}
	var rows []row
	for _, e := range got {
		r := row{facet: e.Facet, ts: *e.TimestampMs, item: e.Item, text: e.Text, typ: e.Type, value: e.Value}
		if e.Confidence != nil {
			r.conf = *e.Confidence
		}
		if e.Kind != contextcore.EvidenceVisual || e.ObservationID != visualID {
			t.Fatalf("item: %+v", e)
		}
		rows = append(rows, r)
	}
	want := []row{
		{facet: contextcore.FacetFrameDescription, ts: 11000, item: 0, text: "a kitchen"},
		{facet: contextcore.FacetDetection, ts: 11000, item: 1, typ: "SCENE", value: "kitchen", conf: 0.6},
		{facet: contextcore.FacetDetection, ts: 11000, item: 2, typ: "OBJECT", value: "pan", conf: 0.5},
		{facet: contextcore.FacetDetection, ts: 13000, item: 1, typ: "BRAND", value: "Acme", conf: 0.4},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("got %+v\nwant %+v", rows, want)
	}
}

func TestCorrelateOrderingAndDeterminism(t *testing.T) {
	in := multimodalInput()
	in.Visual[0].Frames = append([]contracts.VisualFrame{frame(10000, "", det(contracts.VisualDetectionScene, "kitchen", 0.5))}, in.Visual[0].Frames...)
	group := correlate(t, in)
	if group.SegmentID != segment.SegmentID || group.Content != content || group.Window != segment.Window {
		t.Fatalf("group identity: %+v", group)
	}
	var order []string
	for _, e := range group.Evidence {
		ts := int64(-1)
		if e.TimestampMs != nil {
			ts = *e.TimestampMs
		}
		order = append(order, string(e.Kind)+"/"+string(e.Facet)+"/"+e.Value+"@"+itoa(ts))
	}
	want := []string{
		"AUDIO/TRANSCRIPT/@-1",
		"VISUAL/DETECTION/kitchen@10000",
		"VISUAL/FRAME_DESCRIPTION/@11250",
		"VISUAL/DETECTION/olive_oil_bottle@11250",
		"VISUAL/DETECTION/pouring@11250",
		"VISUAL/DETECTION/frying_pan@13750",
	}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order:\n%v\nwant\n%v", order, want)
	}
	first, _ := json.Marshal(group)
	for range 5 {
		again, _ := json.Marshal(correlate(t, in))
		if string(again) != string(first) {
			t.Fatal("correlation is not deterministic")
		}
	}
}

func TestSortEvidenceTieBreaks(t *testing.T) {
	ev := []contextcore.Evidence{
		{Kind: contextcore.EvidenceVisual, ObservationID: "vis:b", TimestampMs: ptr(int64(5)), Item: 0},
		{Kind: contextcore.EvidenceVisual, ObservationID: "vis:a", TimestampMs: ptr(int64(5)), Item: 1},
		{Kind: contextcore.EvidenceVisual, ObservationID: "vis:a", TimestampMs: ptr(int64(5)), Item: 0},
		{Kind: contextcore.EvidenceAudio, ObservationID: "aud:z", Window: contracts.TimeWindow{StartMs: 5, EndMs: 9}},
		{Kind: contextcore.EvidenceAudio, ObservationID: "aud:y", Window: contracts.TimeWindow{StartMs: 6, EndMs: 9}},
	}
	contextcore.SortEvidence(ev)
	var got []string
	for _, e := range ev {
		got = append(got, e.ObservationID+"#"+itoa(int64(e.Item)))
	}
	want := []string{"aud:z#0", "vis:a#0", "vis:a#1", "vis:b#0", "aud:y#0"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestCorrelateDoesNotMutateObservations(t *testing.T) {
	in := multimodalInput()
	before, _ := json.Marshal(in)
	group := correlate(t, in)
	for i := range group.Evidence {
		group.Evidence[i].Text = "changed"
		if group.Evidence[i].Confidence != nil {
			*group.Evidence[i].Confidence = 0
		}
	}
	after, _ := json.Marshal(in)
	if string(before) != string(after) {
		t.Fatalf("observations changed:\n%s\n%s", before, after)
	}
}

func TestCorrelateModalities(t *testing.T) {
	both := multimodalInput()
	audioOnly := contextcore.CorrelationInput{Segment: segment, Audio: both.Audio}
	visionOnly := contextcore.CorrelationInput{Segment: segment, Visual: both.Visual}
	for name, tc := range map[string]struct {
		in           contextcore.CorrelationInput
		audio, video bool
	}{
		"audio only":  {audioOnly, true, false},
		"vision only": {visionOnly, false, true},
		"both":        {both, true, true},
	} {
		t.Run(name, func(t *testing.T) {
			var hasAudio, hasVisual bool
			for _, e := range correlate(t, tc.in).Evidence {
				hasAudio = hasAudio || e.Kind == contextcore.EvidenceAudio
				hasVisual = hasVisual || e.Kind == contextcore.EvidenceVisual
			}
			if hasAudio != tc.audio || hasVisual != tc.video {
				t.Fatalf("audio %t visual %t", hasAudio, hasVisual)
			}
		})
	}
}

func TestCorrelateEmptyGroup(t *testing.T) {
	for name, in := range map[string]contextcore.CorrelationInput{
		"no observations": {Segment: segment},
		"blank transcript and empty frame": {
			Segment: segment,
			Audio:   []contracts.AudioObservation{audioObs("   ")},
			Visual:  []contracts.VisualObservation{visualObs(frame(12500, ""))},
		},
	} {
		groups, err := contextcore.SegmentCorrelator{}.Correlate(in)
		if err != nil || len(groups) != 0 {
			t.Fatalf("%s: groups %v err %v", name, groups, err)
		}
	}
}

func TestCorrelateRejects(t *testing.T) {
	adjacent := contracts.TimeWindow{StartMs: 15000, EndMs: 20000}
	cases := map[string]struct {
		mutate func(*contextcore.CorrelationInput)
		want   string
	}{
		"wrong content": {func(in *contextcore.CorrelationInput) {
			in.Audio[0].Content = contracts.ContentRef{ContentID: "other", ContentType: contracts.ContentTypeVOD}
		}, "does not match segment content"},
		"wrong content type": {func(in *contextcore.CorrelationInput) {
			in.Visual[0].Content.ContentType = contracts.ContentTypeLive
		}, "does not match segment content"},
		"adjacent window sharing an endpoint": {func(in *contextcore.CorrelationInput) {
			in.Audio[0].Window = adjacent
		}, "is not the segment window"},
		"narrower window": {func(in *contextcore.CorrelationInput) {
			in.Visual[0].Window = contracts.TimeWindow{StartMs: 11000, EndMs: 15000}
		}, "is not the segment window"},
		"frame outside window": {func(in *contextcore.CorrelationInput) {
			in.Visual[0].Frames[0].TimestampMs = 16000
		}, "invalid observation"},
		"duplicate audio id": {func(in *contextcore.CorrelationInput) {
			in.Audio = append(in.Audio, in.Audio[0])
		}, "duplicate observationId"},
		"duplicate id across kinds": {func(in *contextcore.CorrelationInput) {
			in.Visual[0].ObservationID = audioID
		}, "duplicate observationId"},
		"duplicate frame timestamp": {func(in *contextcore.CorrelationInput) {
			in.Visual[0].Frames[1].TimestampMs = in.Visual[0].Frames[0].TimestampMs
		}, "duplicate frame timestamp"},
		"blank observation id": {func(in *contextcore.CorrelationInput) {
			in.Audio[0].ObservationID = ""
		}, "observationId must not be blank"},
		"invalid segment": {func(in *contextcore.CorrelationInput) {
			in.Segment.Window = contracts.TimeWindow{StartMs: 5, EndMs: 5}
		}, "invalid segment"},
		"blank segment id": {func(in *contextcore.CorrelationInput) {
			in.Segment.SegmentID = ""
		}, "segment id must not be blank"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			in := multimodalInput()
			tc.mutate(&in)
			_, err := contextcore.SegmentCorrelator{}.Correlate(in)
			var staged *contextcore.Error
			if !errors.As(err, &staged) || staged.Stage != contextcore.StageCorrelate {
				t.Fatalf("want correlate error, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

func TestGround(t *testing.T) {
	group := correlate(t, multimodalInput())
	if err := contextcore.Ground(group, candidate(audioRef(), visualRef(11250), visualRef(13750))); err != nil {
		t.Fatalf("valid candidate: %v", err)
	}
	cases := map[string]struct {
		mutate func(*contextcore.Candidate)
		want   string
	}{
		"no evidence":              {func(c *contextcore.Candidate) { c.Evidence = nil }, "cites no evidence"},
		"unknown observation":      {func(c *contextcore.Candidate) { c.Evidence = []contextcore.EvidenceRef{{ObservationID: "aud:other"}} }, "unknown observationId"},
		"unknown frame":            {func(c *contextcore.Candidate) { c.Evidence = []contextcore.EvidenceRef{visualRef(12500)} }, "unknown frame 12500ms"},
		"visual without timestamp": {func(c *contextcore.Candidate) { c.Evidence = []contextcore.EvidenceRef{{ObservationID: visualID}} }, "without a frame timestamp"},
		"audio with timestamp": {func(c *contextcore.Candidate) {
			c.Evidence = []contextcore.EvidenceRef{{ObservationID: audioID, TimestampMs: ptr(int64(11250))}}
		}, "cited with a timestamp"},
		"duplicate reference": {func(c *contextcore.Candidate) {
			c.Evidence = []contextcore.EvidenceRef{visualRef(11250), visualRef(11250)}
		}, "duplicate reference"},
		"no items": {func(c *contextcore.Candidate) {
			c.Entities, c.Topics, c.Objects, c.Brands = nil, nil, nil, nil
		}, "asserts no entity"},
		"blank entity type":        {func(c *contextcore.Candidate) { c.Entities[0].Type = " " }, "blank type"},
		"blank value":              {func(c *contextcore.Candidate) { c.Topics[0].Value = "" }, "topics[0]: blank value"},
		"duplicate value":          {func(c *contextcore.Candidate) { c.Topics = append(c.Topics, c.Topics[0]) }, "duplicate value"},
		"duplicate entity":         {func(c *contextcore.Candidate) { c.Entities = append(c.Entities, c.Entities[0]) }, "duplicate entity"},
		"missing event confidence": {func(c *contextcore.Candidate) { c.Confidence = nil }, "confidence: missing confidence"},
		"missing item confidence":  {func(c *contextcore.Candidate) { c.Objects[0].Confidence = nil }, "objects[0]: missing confidence"},
		"confidence above one":     {func(c *contextcore.Candidate) { c.Confidence = ptr(1.1) }, "outside [0, 1]"},
		"negative item confidence": {func(c *contextcore.Candidate) { c.Entities[0].Confidence = ptr(-0.1) }, "outside [0, 1]"},
		"blank provider":           {func(c *contextcore.Candidate) { c.Reasoning.Provider = "" }, "provider must not be blank"},
		"model path":               {func(c *contextcore.Candidate) { c.Reasoning.Model = "/models/m.gguf" }, "not a path"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := candidate(audioRef(), visualRef(11250))
			tc.mutate(&c)
			err := contextcore.Ground(group, c)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v does not contain %q", err, tc.want)
			}
		})
	}
}

func TestGroundRejectsCitingUnprojectedEvidence(t *testing.T) {
	in := multimodalInput()
	in.Visual[0].Frames = append(in.Visual[0].Frames, frame(14000, ""))
	blank := audioObs("")
	blank.ObservationID = "aud:blank"
	group := correlate(t, contextcore.CorrelationInput{Segment: segment, Visual: in.Visual, Audio: []contracts.AudioObservation{blank}})
	for _, ref := range []contextcore.EvidenceRef{visualRef(14000), {ObservationID: "aud:blank"}} {
		if err := contextcore.Ground(group, candidate(ref)); err == nil {
			t.Fatalf("citing %+v must be rejected", ref)
		}
	}
}

func newValidator(t *testing.T) *contracts.Validator {
	t.Helper()
	v, err := contracts.NewValidator(os.DirFS("../../specs"))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestNewContextEvent(t *testing.T) {
	in := multimodalInput()
	group := correlate(t, in)
	c := candidate(visualRef(13750), audioRef(), visualRef(11250))
	event, err := contextcore.NewContextEvent(group, c, 2, "poc-v1")
	if err != nil {
		t.Fatal(err)
	}
	want := contracts.ContextEventV1{
		EventID:       "ctx:vod-cooking-001:10000-15000:2",
		SchemaVersion: "1.0",
		Content:       content,
		Window:        segment.Window,
		Context: contracts.ContextBody{
			Entities: []contracts.Entity{{Type: "ACTIVITY", Value: "cooking", Confidence: 0.8}},
			Topics:   []contracts.SemanticValue{{Value: "cooking", Confidence: 0.9}},
			Objects:  []contracts.SemanticValue{{Value: "olive_oil", Confidence: 0.7}},
			Brands:   []contracts.SemanticValue{},
		},
		Confidence: 0.75,
		Evidence: contracts.Evidence{
			Audio: []contracts.AudioEvidence{{ObservationID: audioID, StartMs: 10000, EndMs: 15000, Text: "agora vou colocar o azeite [BLANK_AUDIO]"}},
			Visual: []contracts.VisualEvidence{
				{ObservationID: visualID, TimestampMs: 11250, Description: "hand pouring oil into a pan"},
				{ObservationID: visualID, TimestampMs: 13750, Description: "OBJECT: frying_pan"},
			},
		},
		Provenance: contracts.ContextProvenance{
			PipelineVersion: "poc-v1", FusionProvider: ptr("fake"), FusionModel: ptr("fake-model.gguf"), PromptVersion: ptr("test-v1"),
		},
	}
	if !reflect.DeepEqual(event, want) {
		got, _ := json.MarshalIndent(event, "", " ")
		t.Fatalf("event:\n%s", got)
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if err := newValidator(t).ValidateJSON(contracts.KindContextEventV1, data); err != nil {
		t.Fatalf("schema: %v", err)
	}
	catalog, err := contracts.NewCatalog(in.Audio, in.Visual)
	if err != nil {
		t.Fatal(err)
	}
	if err := event.ValidateEvidence(catalog); err != nil {
		t.Fatalf("evidence: %v", err)
	}
	for _, key := range []string{"productId", "sku", "retailer", "price", "inventory", "offer", "checkout"} {
		if strings.Contains(string(data), `"`+key+`"`) {
			t.Fatalf("commerce key %q in %s", key, data)
		}
	}
}

func TestNewContextEventRendersDetectionsAndKeepsEmptyArrays(t *testing.T) {
	in := contextcore.CorrelationInput{Segment: segment, Visual: []contracts.VisualObservation{visualObs(
		frame(12000, "", det(contracts.VisualDetectionObject, "ball", 0.9), det(contracts.VisualDetectionAction, "kick", 0.8)),
	)}}
	c := contextcore.Candidate{
		Topics:     []contextcore.Value{{Value: "football", Confidence: ptr(0.6)}},
		Confidence: ptr(0.5),
		Evidence:   []contextcore.EvidenceRef{visualRef(12000)},
		Reasoning:  contextcore.ReasoningMetadata{Provider: "fake"},
	}
	event, err := contextcore.NewContextEvent(correlate(t, in), c, 1, "poc-v1")
	if err != nil {
		t.Fatal(err)
	}
	if got := event.Evidence.Visual[0].Description; got != "OBJECT: ball; ACTION: kick" {
		t.Fatalf("description %q", got)
	}
	data, _ := json.Marshal(event)
	for _, want := range []string{`"entities":[]`, `"objects":[]`, `"brands":[]`, `"audio":[]`, `"provenance":{"pipelineVersion":"poc-v1","fusionProvider":"fake"}`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %s in %s", want, data)
		}
	}
	if err := newValidator(t).ValidateJSON(contracts.KindContextEventV1, data); err != nil {
		t.Fatal(err)
	}
}

func TestNewContextEventRejects(t *testing.T) {
	group := correlate(t, multimodalInput())
	if _, err := contextcore.NewContextEvent(group, candidate(visualRef(1)), 1, "poc-v1"); err == nil {
		t.Fatal("ungrounded candidate mapped")
	}
	if _, err := contextcore.NewContextEvent(group, candidate(audioRef()), 0, "poc-v1"); err == nil {
		t.Fatal("ordinal 0 accepted")
	}
	if _, err := contextcore.NewContextEvent(group, candidate(audioRef()), 1, " "); err == nil {
		t.Fatal("blank pipeline version accepted")
	}
}

func TestEventID(t *testing.T) {
	if got := contextcore.EventID("c:0-5000", 3); got != "ctx:c:0-5000:3" {
		t.Fatal(got)
	}
}

func TestRegistry(t *testing.T) {
	r := contextcore.NewRegistry()
	if _, err := r.New("x", nil); !errors.Is(err, contextcore.ErrUnknownProvider) || !strings.Contains(err.Error(), "available: none") {
		t.Fatalf("unknown: %v", err)
	}
	var got contextcore.Options
	ok := func(o contextcore.Options) (contextcore.Reasoner, error) {
		got = o
		return contextcore.ReasonerFunc(func(context.Context, contextcore.EvidenceGroup) ([]contextcore.Candidate, error) { return nil, nil }), nil
	}
	if err := r.Register("b", ok); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("a", ok); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []struct {
		name    string
		factory contextcore.Factory
	}{{"", ok}, {"c", nil}, {"a", ok}} {
		if err := r.Register(bad.name, bad.factory); err == nil {
			t.Fatalf("register %q accepted", bad.name)
		}
	}
	if !reflect.DeepEqual(r.Names(), []string{"a", "b"}) {
		t.Fatal(r.Names())
	}
	if _, err := r.New("a", nil); err != nil || got == nil {
		t.Fatalf("new: %v, options %v", err, got)
	}
	if _, err := r.New("zz", nil); err == nil || !strings.Contains(err.Error(), "available: a, b") {
		t.Fatalf("unknown lists names: %v", err)
	}
	cause := errors.New("boom")
	_ = r.Register("broken", func(contextcore.Options) (contextcore.Reasoner, error) { return nil, cause })
	if _, err := r.New("broken", nil); !errors.Is(err, cause) {
		t.Fatalf("factory error: %v", err)
	}
	_ = r.Register("nil", func(contextcore.Options) (contextcore.Reasoner, error) { return nil, nil })
	if _, err := r.New("nil", nil); err == nil {
		t.Fatal("nil reasoner accepted")
	}
}

// fakeReasoner records calls and returns scripted candidates.
type fakeReasoner struct {
	mu     sync.Mutex
	calls  int
	groups []contextcore.EvidenceGroup
	fn     func(ctx context.Context, g contextcore.EvidenceGroup) ([]contextcore.Candidate, error)
}

func (f *fakeReasoner) Reason(ctx context.Context, g contextcore.EvidenceGroup) ([]contextcore.Candidate, error) {
	f.mu.Lock()
	f.calls++
	f.groups = append(f.groups, g)
	f.mu.Unlock()
	return f.fn(ctx, g)
}

func returning(cs ...contextcore.Candidate) *fakeReasoner {
	return &fakeReasoner{fn: func(context.Context, contextcore.EvidenceGroup) ([]contextcore.Candidate, error) { return cs, nil }}
}

func newEngine(t *testing.T, r contextcore.Reasoner) *contextcore.Engine {
	t.Helper()
	e, err := contextcore.NewEngine("fake", r)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestEngineCandidates(t *testing.T) {
	ctx := context.Background()
	zero := returning()
	if events, err := newEngine(t, zero).Process(ctx, multimodalInput()); err != nil || len(events) != 0 || zero.calls != 1 {
		t.Fatalf("zero candidates: %v %v calls %d", events, err, zero.calls)
	}
	one := returning(candidate(audioRef()))
	events, err := newEngine(t, one).Process(ctx, multimodalInput())
	if err != nil || len(events) != 1 || events[0].EventID != "ctx:"+segment.SegmentID+":1" {
		t.Fatalf("one candidate: %+v %v", events, err)
	}
	second := candidate(visualRef(11250))
	second.Topics = []contextcore.Value{{Value: "kitchen", Confidence: ptr(0.4)}}
	many := returning(candidate(audioRef()), second)
	events, err = newEngine(t, many).Process(ctx, multimodalInput())
	if err != nil || len(events) != 2 || events[1].EventID != "ctx:"+segment.SegmentID+":2" || events[1].Context.Topics[0].Value != "kitchen" {
		t.Fatalf("two candidates: %+v %v", events, err)
	}
}

func TestEngineSkipsReasonerWithoutEvidence(t *testing.T) {
	r := returning(candidate(audioRef()))
	events, err := newEngine(t, r).Process(context.Background(), contextcore.CorrelationInput{
		Segment: segment, Audio: []contracts.AudioObservation{audioObs(" ")},
	})
	if err != nil || events != nil || r.calls != 0 {
		t.Fatalf("events %v err %v calls %d", events, err, r.calls)
	}
}

func TestEngineRejections(t *testing.T) {
	malformed := candidate(audioRef())
	malformed.Confidence = nil
	otherProvider := candidate(audioRef())
	otherProvider.Reasoning.Provider = "someone-else"
	cause := errors.New("provider exploded")
	cases := map[string]struct {
		reasoner *fakeReasoner
		stage    contextcore.Stage
		want     string
		is       error
	}{
		"malformed candidate": {returning(candidate(audioRef()), malformed), contextcore.StageReason, "candidate 2: confidence: missing confidence", nil},
		"unknown reference":   {returning(candidate(contextcore.EvidenceRef{ObservationID: "vis:ghost", TimestampMs: ptr(int64(1))})), contextcore.StageReason, `unknown observationId "vis:ghost"`, nil},
		"unknown timestamp":   {returning(candidate(visualRef(12000))), contextcore.StageReason, "unknown frame 12000ms", nil},
		"provider mismatch":   {returning(otherProvider), contextcore.StageReason, "not the configured provider", nil},
		"provider error": {&fakeReasoner{fn: func(context.Context, contextcore.EvidenceGroup) ([]contextcore.Candidate, error) {
			return nil, cause
		}}, contextcore.StageReason, "provider exploded", cause},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			events, err := newEngine(t, tc.reasoner).Process(context.Background(), multimodalInput())
			var staged *contextcore.Error
			if events != nil || !errors.As(err, &staged) || staged.Stage != tc.stage {
				t.Fatalf("events %v err %v", events, err)
			}
			for _, part := range []string{tc.want, "segment " + segment.SegmentID, "provider fake", "content vod-cooking-001"} {
				if !strings.Contains(err.Error(), part) {
					t.Fatalf("error %q lacks %q", err, part)
				}
			}
			if tc.is != nil && !errors.Is(err, tc.is) {
				t.Fatalf("errors.Is(%v, %v) = false", err, tc.is)
			}
		})
	}
}

func TestEngineCorrelationError(t *testing.T) {
	in := multimodalInput()
	in.Audio[0].Window = contracts.TimeWindow{StartMs: 0, EndMs: 5000}
	r := returning()
	_, err := newEngine(t, r).Process(context.Background(), in)
	var staged *contextcore.Error
	if !errors.As(err, &staged) || staged.Stage != contextcore.StageCorrelate || staged.ObservationID != audioID || r.calls != 0 {
		t.Fatalf("err %v calls %d", err, r.calls)
	}
}

func TestEngineCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := returning(candidate(audioRef()))
	if _, err := newEngine(t, r).Process(ctx, multimodalInput()); !errors.Is(err, ctx.Err()) || r.calls != 0 {
		t.Fatalf("pre-cancelled: %v calls %d", err, r.calls)
	}

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	inFlight := &fakeReasoner{fn: func(ctx context.Context, _ contextcore.EvidenceGroup) ([]contextcore.Candidate, error) {
		cancel()
		return nil, ctx.Err()
	}}
	_, err := newEngine(t, inFlight).Process(ctx, multimodalInput())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("in-flight cancellation not detectable: %v", err)
	}
}

func TestEngineReasonerCannotAlterGrounding(t *testing.T) {
	r := &fakeReasoner{fn: func(_ context.Context, g contextcore.EvidenceGroup) ([]contextcore.Candidate, error) {
		for i := range g.Evidence {
			g.Evidence[i].ObservationID = "vis:forged"
			if g.Evidence[i].TimestampMs != nil {
				*g.Evidence[i].TimestampMs = 1
			}
		}
		return []contextcore.Candidate{candidate(contextcore.EvidenceRef{ObservationID: "vis:forged", TimestampMs: ptr(int64(1))})}, nil
	}}
	in := multimodalInput()
	if _, err := newEngine(t, r).Process(context.Background(), in); err == nil || !strings.Contains(err.Error(), "unknown observationId") {
		t.Fatalf("forged evidence accepted: %v", err)
	}
	if in.Visual[0].Frames[0].TimestampMs != 11250 {
		t.Fatal("observation mutated through the group")
	}
}

func TestNewEngineValidates(t *testing.T) {
	if _, err := contextcore.NewEngine(" ", returning()); err == nil {
		t.Fatal("blank provider accepted")
	}
	if _, err := contextcore.NewEngine("fake", nil); err == nil {
		t.Fatal("nil reasoner accepted")
	}
	if _, err := contextcore.NewEngine("fake", returning(), contextcore.WithPipelineVersion("")); err == nil {
		t.Fatal("blank pipeline version accepted")
	}
	e, err := contextcore.NewEngine("fake", returning(candidate(audioRef())), contextcore.WithPipelineVersion("poc-v2"))
	if err != nil {
		t.Fatal(err)
	}
	events, err := e.Process(context.Background(), multimodalInput())
	if err != nil || events[0].Provenance.PipelineVersion != "poc-v2" {
		t.Fatalf("%v %v", events, err)
	}
}

func TestErrorFormatting(t *testing.T) {
	cause := errors.New("cause")
	err := contextcore.SegmentError(contextcore.StageMapContext, segment, "fake", cause)
	err.ObservationID = audioID
	want := "context map-context: content vod-cooking-001: segment vod-cooking-001:10000-15000: window [10000, 15000]: observation " + audioID + ": provider fake: cause"
	if err.Error() != want || !errors.Is(err, cause) {
		t.Fatalf("%q", err.Error())
	}
	var nilErr *contextcore.Error
	if nilErr.Error() != "" || nilErr.Unwrap() != nil {
		t.Fatal("nil error")
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
