package dataset

import (
	"strings"
	"testing"

	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

func TestGroundTruthTemporalWindows(t *testing.T) {
	cases := []struct {
		name   string
		window contracts.TimeWindow
		want   string
	}{
		{name: "zero length", window: contracts.TimeWindow{StartMs: 5, EndMs: 5}, want: "endMs must be > startMs"},
		{name: "reversed", window: contracts.TimeWindow{StartMs: 5, EndMs: 4}, want: "endMs must be > startMs"},
		{name: "negative", window: contracts.TimeWindow{StartMs: -1, EndMs: 4}, want: "startMs must be >= 0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value := validGroundTruth()
			value.Annotations[0].Window = tc.window
			err := validateGroundTruth(value, "ground-truth.json")
			assertErrorContains(t, err, LayerDomain, tc.want)
		})
	}
}

func TestDuplicateIdentifiers(t *testing.T) {
	t.Run("test case", func(t *testing.T) {
		manifest := validManifest()
		manifest.TestCases = append(manifest.TestCases, manifest.TestCases[0])
		err := validateManifest(manifest, "manifest.json")
		assertErrorContains(t, err, LayerDomain, "duplicate testCaseId")
	})
	t.Run("annotation", func(t *testing.T) {
		groundTruth := validGroundTruth()
		groundTruth.Annotations = append(groundTruth.Annotations, groundTruth.Annotations[0])
		err := validateGroundTruth(groundTruth, "ground-truth.json")
		assertErrorContains(t, err, LayerDomain, "duplicate annotationId")
	})
}

func TestExpectedContextSemanticValidation(t *testing.T) {
	note := "acceptable alternative"
	cases := []struct {
		name   string
		mutate func(*GroundTruthAnnotation)
		want   string
	}{
		{
			name: "blank entity type",
			mutate: func(value *GroundTruthAnnotation) {
				value.Expected.Required = ExpectedSet{
					Entities: []ExpectedEntity{{Type: " ", Value: "Palmeiras"}},
				}
			},
			want: "must not be blank",
		},
		{
			name: "blank string label",
			mutate: func(value *GroundTruthAnnotation) {
				value.Expected.Required = ExpectedSet{Topics: []string{" "}}
			},
			want: "must not be blank",
		},
		{
			name: "duplicate within set",
			mutate: func(value *GroundTruthAnnotation) {
				value.Expected.Required = ExpectedSet{Topics: []string{"football", "football"}}
			},
			want: "duplicate label",
		},
		{
			name: "required optional contradiction",
			mutate: func(value *GroundTruthAnnotation) {
				value.Expected.Required = ExpectedSet{Topics: []string{"football"}}
				value.Expected.Optional = &ExpectedSet{Topics: []string{"football"}}
				value.AmbiguityNote = &note
			},
			want: "both required and optional",
		},
		{
			name: "required forbidden contradiction",
			mutate: func(value *GroundTruthAnnotation) {
				value.Expected.Required = ExpectedSet{Topics: []string{"football"}}
				value.Expected.Forbidden = &ExpectedSet{Topics: []string{"football"}}
			},
			want: "both required and forbidden",
		},
		{
			name: "optional without note",
			mutate: func(value *GroundTruthAnnotation) {
				value.Expected.Optional = &ExpectedSet{Objects: []string{"jersey"}}
			},
			want: "ambiguityNote is required",
		},
		{
			name: "blank ambiguity note",
			mutate: func(value *GroundTruthAnnotation) {
				blank := " "
				value.AmbiguityNote = &blank
			},
			want: "ambiguityNote must not be blank",
		},
		{
			name: "no positive expectation",
			mutate: func(value *GroundTruthAnnotation) {
				value.Expected.Required = ExpectedSet{}
				value.Expected.Forbidden = &ExpectedSet{Topics: []string{"football"}}
			},
			want: "at least one required or optional",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value := validGroundTruth()
			tc.mutate(&value.Annotations[0])
			err := validateGroundTruth(value, "ground-truth.json")
			assertErrorContains(t, err, LayerDomain, tc.want)
		})
	}
}

func TestErrorIncludesLayerSourcePathAndMessage(t *testing.T) {
	err := (&Error{
		Layer: LayerReference, Source: "manifest.json", Path: "testCases[0].media.uri", Message: "not found",
	}).Error()
	for _, part := range []string{LayerReference, "manifest.json", "testCases[0].media.uri", "not found"} {
		if !strings.Contains(err, part) {
			t.Fatalf("%q does not contain %q", err, part)
		}
	}
}
