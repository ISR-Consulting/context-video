package contracts

// ContextEventSchemaVersion is the frozen schemaVersion for ContextEvent v1.
const ContextEventSchemaVersion = "1.0"

// ContextBody is the semantic reading of a context event.
// Event-level confidence is independent of these item confidences.
type ContextBody struct {
	Entities []Entity        `json:"entities"`
	Topics   []SemanticValue `json:"topics"`
	Objects  []SemanticValue `json:"objects"`
	Brands   []SemanticValue `json:"brands"`
}

// AudioEvidence cites an audio observation and the interval that supports the event.
type AudioEvidence struct {
	ObservationID string `json:"observationId"`
	StartMs       int64  `json:"startMs"`
	EndMs         int64  `json:"endMs"`
	Text          string `json:"text"`
}

// VisualEvidence cites a visual observation and a frame timestamp.
type VisualEvidence struct {
	ObservationID string `json:"observationId"`
	TimestampMs   int64  `json:"timestampMs"`
	Description   string `json:"description"`
}

// Evidence holds audio and visual support for a context event.
type Evidence struct {
	Audio  []AudioEvidence  `json:"audio"`
	Visual []VisualEvidence `json:"visual"`
}

// ContextEventV1 is a semantic interpretation over one temporal window.
type ContextEventV1 struct {
	EventID       string            `json:"eventId"`
	SchemaVersion string            `json:"schemaVersion"`
	Content       ContentRef        `json:"content"`
	Window        TimeWindow        `json:"window"`
	Context       ContextBody       `json:"context"`
	Confidence    float64           `json:"confidence"`
	Evidence      Evidence          `json:"evidence"`
	Provenance    ContextProvenance `json:"provenance"`
}
