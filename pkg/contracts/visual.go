package contracts

// VisualDetectionType is the closed set of visual observation types.
type VisualDetectionType string

const (
	VisualDetectionObject VisualDetectionType = "OBJECT"
	VisualDetectionEntity VisualDetectionType = "ENTITY"
	VisualDetectionTopic  VisualDetectionType = "TOPIC"
	VisualDetectionBrand  VisualDetectionType = "BRAND"
	VisualDetectionText   VisualDetectionType = "TEXT"
	VisualDetectionScene  VisualDetectionType = "SCENE"
	VisualDetectionAction VisualDetectionType = "ACTION"
)

// VisualDetection is one structured observation on a sampled frame.
type VisualDetection struct {
	Type       VisualDetectionType `json:"type"`
	Value      string              `json:"value"`
	Confidence float64             `json:"confidence"`
}

// VisualFrame is a sampled frame and the detections attached to it.
type VisualFrame struct {
	TimestampMs  int64             `json:"timestampMs"`
	Description  *string           `json:"description,omitempty"`
	Observations []VisualDetection `json:"observations"`
}

// VisualObservation is source evidence from sampled frames, not semantic context.
type VisualObservation struct {
	ObservationID string                `json:"observationId"`
	Content       ContentRef            `json:"content"`
	Window        TimeWindow            `json:"window"`
	Frames        []VisualFrame         `json:"frames"`
	Provenance    ObservationProvenance `json:"provenance"`
}
