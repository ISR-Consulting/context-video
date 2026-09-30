package llamacpp

import (
	"encoding/json"
	"slices"

	contextcore "github.com/ISR-Consulting/context-video/internal/context"
)

// PromptVersion identifies SystemPrompt and the answer shape. It is recorded
// as provenance.promptVersion of every ContextEvent this adapter supports.
const PromptVersion = "context-reasoning-v1"

// SystemPrompt instructs the model. The evidence of one window follows as the
// user message.
const SystemPrompt = `You are the semantic reasoning component of a video context pipeline. You receive the perception evidence of one time window of one video: audio transcripts and visual frame observations. Decide what is happening in the content during this window, using only this evidence.

Answer with exactly one JSON object: {"events": [...]}. Each event has "entities" [{"type", "value", "confidence"}], "topics" [{"value", "confidence"}], "objects" [{"value", "confidence"}], "brands" [{"value", "confidence"}], an event "confidence" and "evidence" [{"observationId"} for audio, {"observationId", "timestampMs"} for a visual frame].

Rules:
- Use only what the evidence supports. Do not invent people, places, objects or brands.
- contentId, observationId, kind and facet are opaque bookkeeping fields, never content: do not use them as entities, topics, objects or brands.
- Every event must cite the evidence it relies on, using the observationId and, for visual evidence, the timestampMs given in the input.
- Modalities may disagree. You may use audio only, visual only or both. If the evidence is insufficient, answer {"events": []}.
- Entity "type" is a short uppercase category such as PERSON, SPORTS_TEAM, PLACE or ORGANIZATION. Values are short labels.
- A brand is only a brand that is visibly present or mentioned. Never propose products, prices, retailers, offers, recommendations or rankings.
- The "confidence" values in the evidence are uncalibrated model outputs, not probabilities. Do not average them and do not copy them as your confidence.
- Every "confidence" you write is your own estimate, from 0 to 1, that the item or event is supported by the evidence.`

// promptInput is the compact, deterministic evidence document sent to the
// model. Field order is fixed by the struct.
type promptInput struct {
	ContentID string       `json:"contentId"`
	Window    promptWindow `json:"window"`
	Evidence  []promptItem `json:"evidence"`
}

type promptWindow struct {
	StartMs int64 `json:"startMs"`
	EndMs   int64 `json:"endMs"`
}

type promptItem struct {
	Kind          string   `json:"kind"`
	ObservationID string   `json:"observationId"`
	TimestampMs   *int64   `json:"timestampMs,omitempty"`
	Facet         string   `json:"facet"`
	Type          string   `json:"type,omitempty"`
	Value         string   `json:"value,omitempty"`
	Text          string   `json:"text,omitempty"`
	Language      string   `json:"language,omitempty"`
	Confidence    *float64 `json:"confidence,omitempty"`
}

// UserPrompt returns the evidence message for group.
func UserPrompt(group contextcore.EvidenceGroup) ([]byte, error) {
	in := promptInput{
		ContentID: group.Content.ContentID,
		Window:    promptWindow{StartMs: group.Window.StartMs, EndMs: group.Window.EndMs},
		Evidence:  make([]promptItem, 0, len(group.Evidence)),
	}
	for _, e := range group.Evidence {
		in.Evidence = append(in.Evidence, promptItem{
			Kind:          string(e.Kind),
			ObservationID: e.ObservationID,
			TimestampMs:   e.TimestampMs,
			Facet:         string(e.Facet),
			Type:          e.Type,
			Value:         e.Value,
			Text:          e.Text,
			Language:      e.Language,
			Confidence:    e.Confidence,
		})
	}
	data, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	return append([]byte("Evidence:\n"), data...), nil
}

// ResponseSchema returns the JSON schema passed to llama.cpp for group. It
// restricts citations to the group's observation IDs and, per visual
// observation, to the frame timestamps the group holds, so the grammar cannot
// generate a reference to evidence that does not exist. The core grounds
// every candidate again regardless.
func ResponseSchema(group contextcore.EvidenceGroup) ([]byte, error) {
	var audioIDs []string
	var visualIDs []string
	frames := make(map[string][]int64)
	for _, e := range group.Evidence {
		switch e.Kind {
		case contextcore.EvidenceAudio:
			if !slices.Contains(audioIDs, e.ObservationID) {
				audioIDs = append(audioIDs, e.ObservationID)
			}
		case contextcore.EvidenceVisual:
			if e.TimestampMs == nil {
				continue
			}
			if !slices.Contains(visualIDs, e.ObservationID) {
				visualIDs = append(visualIDs, e.ObservationID)
			}
			if !slices.Contains(frames[e.ObservationID], *e.TimestampMs) {
				frames[e.ObservationID] = append(frames[e.ObservationID], *e.TimestampMs)
			}
		}
	}

	var refs []any
	for _, id := range audioIDs {
		refs = append(refs, object([]string{"observationId"}, map[string]any{
			"observationId": map[string]any{"const": id},
		}))
	}
	for _, id := range visualIDs {
		refs = append(refs, object([]string{"observationId", "timestampMs"}, map[string]any{
			"observationId": map[string]any{"const": id},
			"timestampMs":   map[string]any{"enum": frames[id]},
		}))
	}
	evidence := map[string]any{"type": "array", "minItems": 1}
	if len(refs) == 1 {
		evidence["items"] = refs[0]
	} else {
		evidence["items"] = map[string]any{"anyOf": refs}
	}

	confidence := map[string]any{"type": "number", "minimum": 0, "maximum": 1}
	label := map[string]any{"type": "string", "minLength": 1}
	values := map[string]any{"type": "array", "items": object([]string{"value", "confidence"}, map[string]any{
		"value": label, "confidence": confidence,
	})}
	entities := map[string]any{"type": "array", "items": object([]string{"type", "value", "confidence"}, map[string]any{
		"type": label, "value": label, "confidence": confidence,
	})}
	event := object([]string{"entities", "topics", "objects", "brands", "confidence", "evidence"}, map[string]any{
		"entities":   entities,
		"topics":     values,
		"objects":    values,
		"brands":     values,
		"confidence": confidence,
		"evidence":   evidence,
	})
	root := object([]string{"events"}, map[string]any{
		"events": map[string]any{"type": "array", "items": event},
	})
	return json.Marshal(root)
}

func object(required []string, properties map[string]any) map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             required,
		"properties":           properties,
	}
}
