package context

import "context"

// Reasoner interprets one correlated EvidenceGroup. Implementations receive
// evidence only: they must not segment media, transcribe audio, extract frames
// or modify the group. They must honor ctx cancellation and must not return
// provider-native types. Returning no candidates is legitimate when the
// evidence is insufficient.
type Reasoner interface {
	Reason(ctx context.Context, group EvidenceGroup) ([]Candidate, error)
}

// ReasonerFunc adapts a function to Reasoner.
type ReasonerFunc func(ctx context.Context, group EvidenceGroup) ([]Candidate, error)

// Reason calls f.
func (f ReasonerFunc) Reason(ctx context.Context, group EvidenceGroup) ([]Candidate, error) {
	return f(ctx, group)
}
