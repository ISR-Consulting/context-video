//go:build integration

package llamacpp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"

	contextcore "github.com/ISR-Consulting/context-video/internal/context"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// TestIntegrationReason runs the real llama.cpp CLI with the model named by
// CONTEXT_VIDEO_LLM_MODEL over a synthetic audio + visual segment and checks
// that every candidate grounds and maps onto a valid ContextEvent. Zero
// candidates is an acceptable answer. CONTEXT_VIDEO_LLM_BINARY overrides the
// llama-completion binary.
func TestIntegrationReason(t *testing.T) {
	model := os.Getenv("CONTEXT_VIDEO_LLM_MODEL")
	if model == "" {
		t.Skip("CONTEXT_VIDEO_LLM_MODEL not set")
	}
	binary := os.Getenv("CONTEXT_VIDEO_LLM_BINARY")
	if binary == "" {
		binary = defaultBinary
	}
	if _, err := exec.LookPath(binary); err != nil {
		t.Skipf("%s not found: %v", binary, err)
	}
	adapter, err := New(Config{Model: model, Binary: binary})
	if err != nil {
		t.Fatal(err)
	}

	segment := contracts.MediaSegment{
		SegmentID: "live-football-001:0-5000",
		Content:   contracts.ContentRef{ContentID: "live-football-001", ContentType: contracts.ContentTypeLive},
		Window:    contracts.TimeWindow{StartMs: 0, EndMs: 5000},
	}
	description := "football players in green jerseys celebrating on a pitch"
	groups, err := contextcore.SegmentCorrelator{}.Correlate(contextcore.CorrelationInput{
		Segment: segment,
		Audio: []contracts.AudioObservation{{
			ObservationID: "aud:" + segment.SegmentID,
			Content:       segment.Content,
			Window:        segment.Window,
			Transcript:    contracts.Transcript{Text: "gol do Palmeiras! que jogada do camisa dez"},
			Provenance:    contracts.ObservationProvenance{Provider: "fake", PipelineVersion: "poc-v1"},
		}},
		Visual: []contracts.VisualObservation{{
			ObservationID: "vis:" + segment.SegmentID,
			Content:       segment.Content,
			Window:        segment.Window,
			Frames: []contracts.VisualFrame{
				{TimestampMs: 1250, Description: &description, Observations: []contracts.VisualDetection{
					{Type: contracts.VisualDetectionObject, Value: "football_jersey", Confidence: 0.9},
					{Type: contracts.VisualDetectionScene, Value: "football_pitch", Confidence: 0.85},
				}},
				{TimestampMs: 3750, Observations: []contracts.VisualDetection{
					{Type: contracts.VisualDetectionAction, Value: "celebrating", Confidence: 0.8},
				}},
			},
			Provenance: contracts.ObservationProvenance{Provider: "fake", PipelineVersion: "poc-v1"},
		}},
	})
	if err != nil || len(groups) != 1 {
		t.Fatalf("correlate: %v, %d groups", err, len(groups))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	candidates, err := adapter.Reason(ctx, groups[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d candidate(s)", len(candidates))
	validator, err := contracts.NewValidator(os.DirFS("../../../specs"))
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range candidates {
		event, err := contextcore.NewContextEvent(groups[0], c, i+1, "poc-v1")
		if err != nil {
			t.Fatalf("candidate %d: %v", i+1, err)
		}
		data, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		if err := validator.ValidateJSON(contracts.KindContextEventV1, data); err != nil {
			t.Fatalf("candidate %d: %v", i+1, err)
		}
		t.Logf("event %d: %s", i+1, data)
	}
}
