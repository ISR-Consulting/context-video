package dataset

import "github.com/ISR-Consulting/context-video/pkg/contracts"

const SchemaVersion = "1.0"

// Scenario identifies one of the initial POC evaluation scenarios.
type Scenario string

const (
	ScenarioFootballSportsApparel Scenario = "FOOTBALL_SPORTS_APPAREL"
	ScenarioPredominantlyVisual   Scenario = "PREDOMINANTLY_VISUAL"
	ScenarioPredominantlyAuditory Scenario = "PREDOMINANTLY_AUDITORY"
	ScenarioMultimodalAmbiguous   Scenario = "MULTIMODAL_AMBIGUOUS"
)

// MediaKind describes how media bytes are made available to the loader.
type MediaKind string

const (
	MediaKindLocal            MediaKind = "LOCAL"
	MediaKindFixture          MediaKind = "FIXTURE"
	MediaKindControlledSource MediaKind = "CONTROLLED_SOURCE"
)

// Manifest is the versioned entry point for a golden dataset.
type Manifest struct {
	SchemaVersion               string     `json:"schemaVersion"`
	DatasetID                   string     `json:"datasetId"`
	DatasetVersion              string     `json:"datasetVersion"`
	Name                        string     `json:"name"`
	Description                 *string    `json:"description,omitempty"`
	AnnotationGuidelinesVersion string     `json:"annotationGuidelinesVersion"`
	TestCases                   []TestCase `json:"testCases"`
}

// TestCase identifies one reproducible evaluation scenario.
type TestCase struct {
	TestCaseID      string               `json:"testCaseId"`
	Scenario        Scenario             `json:"scenario"`
	Content         contracts.ContentRef `json:"content"`
	Media           MediaReference       `json:"media"`
	GroundTruthPath string               `json:"groundTruthPath"`
	Description     *string              `json:"description,omitempty"`
	Tags            []string             `json:"tags,omitempty"`
}

// MediaReference pins the identity and duration of scenario media.
type MediaReference struct {
	Kind       MediaKind `json:"kind"`
	URI        string    `json:"uri"`
	SHA256     string    `json:"sha256"`
	DurationMs int64     `json:"durationMs"`
}

// GroundTruth is a human-authored expected semantic timeline.
type GroundTruth struct {
	SchemaVersion string                  `json:"schemaVersion"`
	TestCaseID    string                  `json:"testCaseId"`
	Content       contracts.ContentRef    `json:"content"`
	Annotations   []GroundTruthAnnotation `json:"annotations"`
}

// GroundTruthAnnotation describes expected context in one media window.
type GroundTruthAnnotation struct {
	AnnotationID  string               `json:"annotationId"`
	Window        contracts.TimeWindow `json:"window"`
	Expected      ExpectedContext      `json:"expected"`
	AmbiguityNote *string              `json:"ambiguityNote,omitempty"`
	AnnotatorNote *string              `json:"annotatorNote,omitempty"`
}

// ExpectedContext groups required, acceptable, and forbidden labels.
type ExpectedContext struct {
	Required  ExpectedSet  `json:"required"`
	Optional  *ExpectedSet `json:"optional,omitempty"`
	Forbidden *ExpectedSet `json:"forbidden,omitempty"`
}

// ExpectedSet contains dataset labels without model confidence.
type ExpectedSet struct {
	Entities []ExpectedEntity `json:"entities,omitempty"`
	Topics   []string         `json:"topics,omitempty"`
	Objects  []string         `json:"objects,omitempty"`
	Brands   []string         `json:"brands,omitempty"`
}

// ExpectedEntity is a typed human-authored entity label.
type ExpectedEntity struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// Dataset is a fully loaded manifest and its ground truths in manifest order.
type Dataset struct {
	Manifest  Manifest
	TestCases []LoadedTestCase
}

// LoadedTestCase pairs a manifest test case with its validated ground truth.
type LoadedTestCase struct {
	TestCase    TestCase
	GroundTruth GroundTruth
}
