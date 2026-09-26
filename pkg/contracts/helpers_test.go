package contracts_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

func specsFS(t *testing.T) fs.FS {
	t.Helper()
	root := filepath.Join("..", "..", "specs")
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		t.Fatalf("specs directory: %v", err)
	}
	return os.DirFS(root)
}

func newValidator(t *testing.T) *contracts.Validator {
	t.Helper()
	v, err := contracts.NewValidator(specsFS(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	return v
}

func unmarshal(data []byte, dest any) error {
	return json.Unmarshal(data, dest)
}

func f64(v float64) *float64 { return &v }

func strPtr(v string) *string { return &v }

func liveContent() contracts.ContentRef {
	return contracts.ContentRef{ContentID: "live-xyz", ContentType: contracts.ContentTypeLive}
}

func sampleAudio() contracts.AudioObservation {
	return contracts.AudioObservation{
		ObservationID: "aud-01",
		Content:       liveContent(),
		Window:        contracts.TimeWindow{StartMs: 142000, EndMs: 147000},
		Transcript: contracts.Transcript{
			Text:       "essa é a nova camisa do Palmeiras",
			Language:   strPtr("pt-BR"),
			Confidence: f64(0.96),
		},
		Provenance: contracts.ObservationProvenance{
			Provider:        "TBD",
			Model:           strPtr("TBD"),
			PipelineVersion: "poc-v1",
		},
	}
}

func sampleVisual() contracts.VisualObservation {
	desc := "Person wearing a green football jersey"
	return contracts.VisualObservation{
		ObservationID: "vis-01",
		Content:       liveContent(),
		Window:        contracts.TimeWindow{StartMs: 142000, EndMs: 147000},
		Frames: []contracts.VisualFrame{{
			TimestampMs: 144200,
			Description: &desc,
			Observations: []contracts.VisualDetection{{
				Type:       contracts.VisualDetectionObject,
				Value:      "football_jersey",
				Confidence: 0.91,
			}},
		}},
		Provenance: contracts.ObservationProvenance{
			Provider:        "TBD",
			PipelineVersion: "poc-v1",
		},
	}
}

func sampleEvent() contracts.ContextEventV1 {
	fusionProvider := "TBD"
	fusionModel := "TBD"
	prompt := "context-fusion-v1"
	return contracts.ContextEventV1{
		EventID:       "ctx-983472",
		SchemaVersion: contracts.ContextEventSchemaVersion,
		Content:       liveContent(),
		Window:        contracts.TimeWindow{StartMs: 142000, EndMs: 147000},
		Context: contracts.ContextBody{
			Entities: []contracts.Entity{{Type: "SPORTS_TEAM", Value: "Palmeiras", Confidence: 0.96}},
			Topics: []contracts.SemanticValue{
				{Value: "football", Confidence: 0.98},
				{Value: "sports_apparel", Confidence: 0.91},
			},
			Objects: []contracts.SemanticValue{{Value: "football_jersey", Confidence: 0.94}},
			Brands:  []contracts.SemanticValue{},
		},
		Confidence: 0.94,
		Evidence: contracts.Evidence{
			Audio: []contracts.AudioEvidence{{
				ObservationID: "aud-01",
				StartMs:       142000,
				EndMs:         147000,
				Text:          "essa é a nova camisa do Palmeiras",
			}},
			Visual: []contracts.VisualEvidence{{
				ObservationID: "vis-01",
				TimestampMs:   144200,
				Description:   "Palmeiras football jersey visible",
			}},
		},
		Provenance: contracts.ContextProvenance{
			PipelineVersion: "poc-v1",
			FusionProvider:  &fusionProvider,
			FusionModel:     &fusionModel,
			PromptVersion:   &prompt,
		},
	}
}
