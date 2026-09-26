package contracts

// Transcript is the speech text attached to an audio observation.
type Transcript struct {
	Text       string   `json:"text"`
	Language   *string  `json:"language,omitempty"`
	Confidence *float64 `json:"confidence,omitempty"`
}
