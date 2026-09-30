package context

// Entity is a typed semantic value a reasoner concluded. Type is an open
// string, not the visual detection enum.
type Entity struct {
	Type       string
	Value      string
	Confidence *float64
}

// Value is a semantic topic, object or brand a reasoner concluded.
type Value struct {
	Value      string
	Confidence *float64
}

// ReasoningMetadata describes how a candidate was produced. It becomes the
// ContextEvent provenance.
type ReasoningMetadata struct {
	// Provider is the registered reasoner name.
	Provider string
	// Model is a descriptive model label, never a host path. Empty if unknown.
	Model string
	// PromptVersion identifies the reasoning prompt. Empty if not applicable.
	PromptVersion string
}

// Candidate is one semantic conclusion a reasoner drew from an EvidenceGroup.
// It is internal, not a contract. Content and window are deliberately absent:
// they always come from the group, so a reasoner cannot move a conclusion in
// time or onto other content.
//
// Every confidence is the reasoner's own stated 0..1 estimate. It is
// uncalibrated and is never derived from perception confidences.
type Candidate struct {
	Entities []Entity
	Topics   []Value
	Objects  []Value
	Brands   []Value
	// Confidence is the event-level confidence stated by the reasoner.
	Confidence *float64
	// Evidence cites the group evidence supporting the conclusion.
	Evidence  []EvidenceRef
	Reasoning ReasoningMetadata
}
