package contracts

// AudioObservation is source evidence from audio, not semantic context.
type AudioObservation struct {
	ObservationID string                `json:"observationId"`
	Content       ContentRef            `json:"content"`
	Window        TimeWindow            `json:"window"`
	Transcript    Transcript            `json:"transcript"`
	Provenance    ObservationProvenance `json:"provenance"`
}
