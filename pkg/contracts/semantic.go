package contracts

// SemanticValue is a labeled value with an item-level confidence.
type SemanticValue struct {
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence"`
}

// Entity is a typed semantic value on a context event.
// Type is an open string. It is not the visual detection enum.
type Entity struct {
	Type       string  `json:"type"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence"`
}
