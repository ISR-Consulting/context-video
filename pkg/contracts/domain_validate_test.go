package contracts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

func TestTimeWindowBoundaries(t *testing.T) {
	if err := contracts.Validate(contracts.TimeWindow{StartMs: 0, EndMs: 1}); err != nil {
		t.Fatal(err)
	}
	if err := contracts.Validate(&contracts.TimeWindow{StartMs: 0, EndMs: 1}); err != nil {
		t.Fatal(err)
	}
	err := contracts.Validate(contracts.TimeWindow{StartMs: 5, EndMs: 5})
	if err == nil || !strings.Contains(err.Error(), "endMs must be > startMs") {
		t.Fatalf("equal: %v", err)
	}
	err = contracts.Validate(contracts.TimeWindow{StartMs: 5, EndMs: 4})
	if err == nil || !strings.Contains(err.Error(), "endMs must be > startMs") {
		t.Fatalf("reversed: %v", err)
	}
	err = contracts.Validate(contracts.TimeWindow{StartMs: -1, EndMs: 1})
	if err == nil || !strings.Contains(err.Error(), "startMs must be >= 0") {
		t.Fatalf("negative: %v", err)
	}
}

func TestAudioEvidenceInterval(t *testing.T) {
	event := sampleEvent()
	event.Evidence.Visual = nil
	event.Evidence.Audio[0].EndMs = event.Evidence.Audio[0].StartMs
	err := contracts.Validate(event)
	if err == nil || !strings.Contains(err.Error(), "evidence.audio[0].endMs") {
		t.Fatalf("equal evidence interval: %v", err)
	}

	event = sampleEvent()
	event.Evidence.Visual = nil
	event.Evidence.Audio[0].StartMs = 5
	event.Evidence.Audio[0].EndMs = 4
	err = contracts.Validate(event)
	if err == nil || !strings.Contains(err.Error(), "endMs must be > startMs") {
		t.Fatalf("reversed evidence interval: %v", err)
	}
}

func TestEvidenceWindowConsistency(t *testing.T) {
	event := sampleEvent()
	if err := contracts.Validate(event); err != nil {
		t.Fatal(err)
	}

	// Closed intervals: sharing an endpoint overlaps and belongs.
	touch := sampleEvent()
	touch.Evidence.Audio[0].StartMs = 147000
	touch.Evidence.Audio[0].EndMs = 148000
	touch.Evidence.Visual[0].TimestampMs = 147000
	if err := contracts.Validate(touch); err != nil {
		t.Fatalf("endpoint touch: %v", err)
	}
	startTouch := sampleEvent()
	startTouch.Evidence.Visual[0].TimestampMs = 142000
	if err := contracts.Validate(startTouch); err != nil {
		t.Fatalf("start endpoint: %v", err)
	}

	outside := sampleEvent()
	outside.Evidence.Audio[0].StartMs = 147001
	outside.Evidence.Audio[0].EndMs = 148000
	err := contracts.Validate(outside)
	if err == nil || !strings.Contains(err.Error(), "must overlap") {
		t.Fatalf("audio outside: %v", err)
	}

	before := sampleEvent()
	before.Evidence.Visual[0].TimestampMs = 141999
	err = contracts.Validate(before)
	if err == nil || !strings.Contains(err.Error(), "must belong") {
		t.Fatalf("visual before: %v", err)
	}
	after := sampleEvent()
	after.Evidence.Visual[0].TimestampMs = 147001
	err = contracts.Validate(after)
	if err == nil || !strings.Contains(err.Error(), "must belong") {
		t.Fatalf("visual after: %v", err)
	}

	negative := sampleEvent()
	negative.Evidence.Visual[0].TimestampMs = -1
	err = contracts.Validate(negative)
	if err == nil || !strings.Contains(err.Error(), "timestampMs must be >= 0") {
		t.Fatalf("negative timestamp: %v", err)
	}
}

func TestContextEventRequiresEvidence(t *testing.T) {
	event := sampleEvent()
	event.Evidence = contracts.Evidence{Audio: []contracts.AudioEvidence{}, Visual: []contracts.VisualEvidence{}}
	err := contracts.Validate(event)
	if err == nil || !strings.Contains(err.Error(), "at least one audio or visual evidence item is required") {
		t.Fatalf("empty evidence: %v", err)
	}
	catalog, err := contracts.NewCatalog(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Reference validation is independent of the event's semantic validation.
	if err := event.ValidateEvidence(catalog); err != nil {
		t.Fatal(err)
	}
	if err := event.ValidateEvidence(contracts.Catalog{}); err != nil {
		t.Fatal(err)
	}
}

func TestObservationReferences(t *testing.T) {
	audio := sampleAudio()
	visual := sampleVisual()
	event := sampleEvent()
	catalog, err := contracts.NewCatalog([]contracts.AudioObservation{audio}, []contracts.VisualObservation{visual})
	if err != nil {
		t.Fatal(err)
	}
	if err := event.ValidateEvidence(catalog); err != nil {
		t.Fatal(err)
	}

	missing := sampleEvent()
	missing.Evidence.Audio[0].ObservationID = "missing"
	err = missing.ValidateEvidence(catalog)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing: %v", err)
	}

	err = sampleEvent().ValidateEvidence(contracts.Catalog{})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("empty catalog: %v", err)
	}

	visualOnly, err := contracts.NewCatalog(nil, []contracts.VisualObservation{visual})
	if err != nil {
		t.Fatal(err)
	}
	audioCitesVisual := sampleEvent()
	audioCitesVisual.Evidence.Audio[0].ObservationID = visual.ObservationID
	err = audioCitesVisual.ValidateEvidence(visualOnly)
	if err == nil || !strings.Contains(err.Error(), "refers to a visual observation") {
		t.Fatalf("audio id on visual: %v", err)
	}

	audioOnly, err := contracts.NewCatalog([]contracts.AudioObservation{audio}, nil)
	if err != nil {
		t.Fatal(err)
	}
	visualCitesAudio := sampleEvent()
	visualCitesAudio.Evidence.Visual[0].ObservationID = audio.ObservationID
	err = visualCitesAudio.ValidateEvidence(audioOnly)
	if err == nil || !strings.Contains(err.Error(), "refers to an audio observation") {
		t.Fatalf("visual id on audio: %v", err)
	}

	badID := audio
	badID.Content.ContentID = "other"
	mismatched, err := contracts.NewCatalog([]contracts.AudioObservation{badID}, []contracts.VisualObservation{visual})
	if err != nil {
		t.Fatal(err)
	}
	err = event.ValidateEvidence(mismatched)
	if err == nil || !strings.Contains(err.Error(), "content identity") {
		t.Fatalf("content id: %v", err)
	}

	badType := visual
	badType.Content.ContentType = contracts.ContentTypeVOD
	mismatchedType, err := contracts.NewCatalog([]contracts.AudioObservation{audio}, []contracts.VisualObservation{badType})
	if err != nil {
		t.Fatal(err)
	}
	err = event.ValidateEvidence(mismatchedType)
	if err == nil || !strings.Contains(err.Error(), "content identity") {
		t.Fatalf("content type: %v", err)
	}
}

func TestCatalogRejectsDuplicatesAndInvalidObservations(t *testing.T) {
	audio := sampleAudio()
	_, err := contracts.NewCatalog([]contracts.AudioObservation{audio, audio}, nil)
	if err == nil || !strings.Contains(err.Error(), "duplicate observationId") {
		t.Fatalf("duplicate audio: %v", err)
	}
	visual := sampleVisual()
	_, err = contracts.NewCatalog(nil, []contracts.VisualObservation{visual, visual})
	if err == nil || !strings.Contains(err.Error(), "duplicate observationId") {
		t.Fatalf("duplicate visual: %v", err)
	}
	visual.ObservationID = audio.ObservationID
	_, err = contracts.NewCatalog([]contracts.AudioObservation{audio}, []contracts.VisualObservation{visual})
	if err == nil || !strings.Contains(err.Error(), "duplicate observationId") {
		t.Fatalf("duplicate across observation kinds: %v", err)
	}

	broken := audio
	broken.Window.EndMs = broken.Window.StartMs
	_, err = contracts.NewCatalog([]contracts.AudioObservation{broken}, nil)
	if err == nil || !strings.Contains(err.Error(), "endMs must be > startMs") {
		t.Fatalf("invalid observation: %v", err)
	}
}

func TestVisualFrameWindowConsistency(t *testing.T) {
	visual := sampleVisual()
	if err := contracts.Validate(visual); err != nil {
		t.Fatal(err)
	}

	visual.Frames[0].TimestampMs = visual.Window.StartMs
	if err := contracts.Validate(visual); err != nil {
		t.Fatalf("start endpoint: %v", err)
	}
	visual.Frames[0].TimestampMs = visual.Window.EndMs
	if err := contracts.Validate(visual); err != nil {
		t.Fatalf("end endpoint: %v", err)
	}

	visual.Frames[0].TimestampMs = visual.Window.EndMs + 1
	err := contracts.Validate(visual)
	if err == nil || !strings.Contains(err.Error(), "frame timestamp must belong to the observation window") {
		t.Fatalf("outside window: %v", err)
	}

	visual.Frames[0].TimestampMs = -1
	err = contracts.Validate(visual)
	if err == nil || !strings.Contains(err.Error(), "timestampMs must be >= 0") {
		t.Fatalf("negative timestamp: %v", err)
	}
}

func TestCommercialKeys(t *testing.T) {
	keys := []string{"productId", "sku", "SKU", "retailer", "price", "inventory", "offer", "commercialRanking", "checkout"}
	for _, key := range keys {
		segment := contracts.MediaSegment{
			SegmentID: "seg-01",
			Content:   liveContent(),
			Window:    contracts.TimeWindow{StartMs: 0, EndMs: 1},
			Metadata:  map[string]any{key: "x"},
		}
		err := contracts.Validate(segment)
		if err == nil || !strings.Contains(err.Error(), "commercial field is not allowed") || !strings.Contains(err.Error(), key) {
			t.Errorf("metadata %s: %v", key, err)
		}

		result := contracts.ExperimentResult{
			ExperimentID:  "E01",
			StartedAt:     time.Date(2026, 9, 26, 11, 41, 0, 0, time.UTC),
			Configuration: map[string]any{key: "x"},
		}
		err = contracts.Validate(result)
		if err == nil || !strings.Contains(err.Error(), "commercial field is not allowed") || !strings.Contains(err.Error(), key) {
			t.Errorf("configuration %s: %v", key, err)
		}
	}

	nested := contracts.MediaSegment{
		SegmentID: "seg-01",
		Content:   liveContent(),
		Window:    contracts.TimeWindow{StartMs: 0, EndMs: 1},
		Metadata: map[string]any{
			"editorial": map[string]any{
				"items": []any{map[string]any{"price": 10}},
			},
		},
	}
	err := contracts.Validate(nested)
	if err == nil || !strings.Contains(err.Error(), "metadata.editorial.items[0].price") {
		t.Fatalf("nested: %v", err)
	}

	allowed := contracts.MediaSegment{
		SegmentID: "seg-01",
		Content:   liveContent(),
		Window:    contracts.TimeWindow{StartMs: 0, EndMs: 1},
		Metadata:  map[string]any{"title": "sample", "Price": "not the forbidden key"},
	}
	if err := contracts.Validate(allowed); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join("testdata", "schema", "valid", "experiment-product-id.json"))
	if err != nil {
		t.Fatal(err)
	}
	var result contracts.ExperimentResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	err = contracts.Validate(result)
	if err == nil || !strings.Contains(err.Error(), "configuration.nested.sku") {
		t.Fatalf("experiment json: %v", err)
	}
}

func TestMetricZeroRoundTrip(t *testing.T) {
	v := newValidator(t)
	result := contracts.ExperimentResult{
		ExperimentID:  "E04",
		StartedAt:     time.Date(2026, 9, 26, 11, 41, 0, 0, time.UTC),
		Configuration: map[string]any{},
		Metrics: contracts.ExperimentMetrics{
			CostPerVideoHour: f64(0),
			ContextStability: f64(0),
			SchemaCompliance: f64(1),
		},
	}
	if err := contracts.Validate(result); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"costPerVideoHour":0`) || !strings.Contains(string(out), `"contextStability":0`) {
		t.Fatalf("zero omitted: %s", out)
	}
	if err := v.ValidateJSON(contracts.KindExperimentResult, out); err != nil {
		t.Fatalf("schema: %v\n%s", err, out)
	}
	var back contracts.ExperimentResult
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if back.Metrics.CostPerVideoHour == nil || *back.Metrics.CostPerVideoHour != 0 {
		t.Fatalf("cost: %#v", back.Metrics.CostPerVideoHour)
	}
	if back.Metrics.LatencyP50Ms != nil {
		t.Fatalf("absent metric present: %#v", back.Metrics.LatencyP50Ms)
	}
}

func TestVODAndEmptyBrands(t *testing.T) {
	event := sampleEvent()
	event.Content.ContentType = contracts.ContentTypeVOD
	event.Context.Brands = []contracts.SemanticValue{}
	if err := contracts.Validate(event); err != nil {
		t.Fatal(err)
	}
	v := newValidator(t)
	out, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.ValidateJSON(contracts.KindContextEventV1, out); err != nil {
		t.Fatalf("schema: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `"brands":[]`) {
		t.Fatalf("brands omitted: %s", out)
	}
}

func TestMediaSegmentWindow(t *testing.T) {
	segment := contracts.MediaSegment{
		SegmentID: "seg-01",
		Content:   liveContent(),
		Window:    contracts.TimeWindow{StartMs: 0, EndMs: 0},
	}
	err := contracts.Validate(segment)
	if err == nil || !strings.Contains(err.Error(), "window.endMs") {
		t.Fatalf("got %v", err)
	}
}
