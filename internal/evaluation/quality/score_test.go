package quality_test

import (
	"testing"

	"github.com/ISR-Consulting/context-video/internal/evaluation/dataset"
	"github.com/ISR-Consulting/context-video/internal/evaluation/quality"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

func TestScoreCaseLexicalOverlap(t *testing.T) {
	gt := dataset.GroundTruth{
		SchemaVersion: "1.0",
		TestCaseID:    "demo",
		Content:       contracts.ContentRef{ContentID: "c1", ContentType: "VOD"},
		Annotations: []dataset.GroundTruthAnnotation{
			{
				AnnotationID: "a1",
				Window:       contracts.TimeWindow{StartMs: 0, EndMs: 30000},
				Expected: dataset.ExpectedContext{
					Required: dataset.ExpectedSet{
						Topics:  []string{"football"},
						Objects: []string{"football_jersey"},
						Brands:  []string{"adidas"},
					},
					Optional: &dataset.ExpectedSet{
						Topics: []string{"goal"},
					},
					Forbidden: &dataset.ExpectedSet{
						Topics: []string{"basketball"},
					},
				},
			},
		},
	}

	events := []contracts.ContextEventV1{
		{
			EventID: "e1",
			Window:  contracts.TimeWindow{StartMs: 0, EndMs: 5000},
			Context: contracts.ContextBody{
				Topics:  []contracts.SemanticValue{{Value: "football", Confidence: 0.9}},
				Objects: []contracts.SemanticValue{{Value: "football_jersey", Confidence: 0.8}},
				Brands:  []contracts.SemanticValue{{Value: "nike", Confidence: 0.5}},
			},
		},
		{
			EventID: "e2",
			Window:  contracts.TimeWindow{StartMs: 40000, EndMs: 45000}, // outside annotation
			Context: contracts.ContextBody{
				Topics: []contracts.SemanticValue{{Value: "basketball", Confidence: 0.9}},
				Brands: []contracts.SemanticValue{{Value: "adidas", Confidence: 0.9}},
			},
		},
	}

	got := quality.ScoreCase(gt, events)
	if got.ClipRequiredHits != 2 || got.ClipRequiredTotal != 3 {
		t.Fatalf("clip required hits/total = %d/%d, want 2/3", got.ClipRequiredHits, got.ClipRequiredTotal)
	}
	if got.ClipForbiddenHits != 0 {
		t.Fatalf("forbidden hits = %d, want 0 (basketball event does not overlap)", got.ClipForbiddenHits)
	}
	if got.ByFamily[quality.FamilyBrand].RequiredHits != 0 {
		t.Fatalf("adidas must miss: overlapping events have nike only")
	}
	if len(got.MissingRequired) != 1 || got.MissingRequired[0].Value != "adidas" {
		t.Fatalf("missing = %#v, want adidas", got.MissingRequired)
	}
}

func TestScoreCaseForbiddenOnOverlap(t *testing.T) {
	gt := dataset.GroundTruth{
		TestCaseID: "demo",
		Annotations: []dataset.GroundTruthAnnotation{{
			AnnotationID: "a1",
			Window:       contracts.TimeWindow{StartMs: 0, EndMs: 10000},
			Expected: dataset.ExpectedContext{
				Required:  dataset.ExpectedSet{Topics: []string{"cooking"}},
				Forbidden: &dataset.ExpectedSet{Topics: []string{"basketball"}},
			},
		}},
	}
	events := []contracts.ContextEventV1{{
		Window: contracts.TimeWindow{StartMs: 0, EndMs: 5000},
		Context: contracts.ContextBody{
			Topics: []contracts.SemanticValue{
				{Value: "cooking", Confidence: 1},
				{Value: "basketball", Confidence: 0.2},
			},
		},
	}}
	got := quality.ScoreCase(gt, events)
	if got.ClipForbiddenHits != 1 {
		t.Fatalf("forbidden hits = %d, want 1", got.ClipForbiddenHits)
	}
	if got.ClipRequiredRecall != 1 {
		t.Fatalf("recall = %v, want 1", got.ClipRequiredRecall)
	}
}

func TestScoreCaseNoSynonymNormalization(t *testing.T) {
	gt := dataset.GroundTruth{
		TestCaseID: "demo",
		Annotations: []dataset.GroundTruthAnnotation{{
			AnnotationID: "a1",
			Window:       contracts.TimeWindow{StartMs: 0, EndMs: 10000},
			Expected: dataset.ExpectedContext{
				Required: dataset.ExpectedSet{Brands: []string{"podpah"}},
			},
		}},
	}
	events := []contracts.ContextEventV1{{
		Window:  contracts.TimeWindow{StartMs: 0, EndMs: 5000},
		Context: contracts.ContextBody{Brands: []contracts.SemanticValue{{Value: "podpah_tv", Confidence: 1}}},
	}}
	got := quality.ScoreCase(gt, events)
	if got.ClipRequiredHits != 0 {
		t.Fatalf("synonym-like brand must not count as a hit under lexical policy")
	}
}
