package openai

import (
	"encoding/json"
	"strconv"

	contextcore "github.com/ISR-Consulting/context-video/internal/context"
)

// PromptVersion matches the local llama-cpp context-reasoning-v2 prompt.
const PromptVersion = "context-reasoning-v2"

// EntityTypes is the closed entity-type list of the Golden Dataset annotation guidelines.
var EntityTypes = []string{"PERSON", "SPORTS_TEAM", "ORGANIZATION", "PLACE", "EVENT"}

const labelPattern = "^[a-z0-9]+(_[a-z0-9]+)*$"

// SystemPrompt instructs the model. The sources of one window follow as the user message.
const SystemPrompt = `You are the semantic reasoning component of a video context pipeline. You receive the perception evidence of one time window of one video as numbered sources: audio transcripts (a1, a2, ...) and video frames (v1, v2, ...) with a description and detections. Decide what is happening in the content during this window, using only these sources.

Answer with exactly one JSON object: {"events": [...]}. Each event has "entities" [{"type", "value", "confidence", "sources"}], "topics", "objects" and "brands" [{"value", "confidence", "sources"}], and an event "confidence". Usually one event per window is enough; add another only when the sources clearly show something separate happening.

Labels:
- Topics, objects and brands are short English labels in lowercase snake_case (football, football_jersey, sports_apparel, adidas), even when the sources are in Portuguese or another language. Translate the meaning; never copy on-screen text, scores, clocks or captions as a label.
- Entities are named people, teams, organizations, places or events. "type" is one of PERSON, SPORTS_TEAM, ORGANIZATION, PLACE, EVENT. Keep names as written in the sources (Palmeiras, Florian Wirtz), except countries and national teams, which use their English name (Germany, Serbia). Roles such as goalkeeper, player or man are not entities.
- A brand is a brand visibly shown or explicitly mentioned. The TV channel or broadcaster showing the video is not a brand of the content.
- Each label appears at most once in an event.

Sources:
- Every item lists in "sources" every source that supports it. If a label is heard in a transcript and also seen in a frame, cite both. A label taken from a frame must cite that frame.
- Modalities may disagree. You may use audio only, frames only or both. If the sources are insufficient, answer {"events": []}.

Confidence:
- Every "confidence" is your own judgement, from 0 to 1, of how well the cited sources support that item or the event. Judge each item on its own; do not give every item the same value. Perception confidences are uncalibrated model outputs, not probabilities, and are withheld from the sources, so there is no score to copy.

Never propose products, prices, retailers, offers, recommendations or rankings.`

type source struct {
	key string
	ref contextcore.EvidenceRef
	doc promptSource
}

type promptInput struct {
	Window  promptWindow   `json:"window"`
	Sources []promptSource `json:"sources"`
}

type promptWindow struct {
	StartMs int64 `json:"startMs"`
	EndMs   int64 `json:"endMs"`
}

type promptSource struct {
	Source      string            `json:"source"`
	Kind        string            `json:"kind"`
	TimestampMs *int64            `json:"timestampMs,omitempty"`
	Transcript  string            `json:"transcript,omitempty"`
	Language    string            `json:"language,omitempty"`
	Description string            `json:"description,omitempty"`
	Detections  []promptDetection `json:"detections,omitempty"`
}

type promptDetection struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

func sources(group contextcore.EvidenceGroup) []source {
	var out []source
	index := map[string]int{}
	audio, visual := 0, 0
	for _, e := range group.Evidence {
		var id string
		if e.Kind == contextcore.EvidenceVisual {
			if e.TimestampMs == nil {
				continue
			}
			id = e.ObservationID + "@" + strconv.FormatInt(*e.TimestampMs, 10)
		} else {
			id = e.ObservationID
		}
		i, ok := index[id]
		if !ok {
			s := source{ref: e.Ref(), doc: promptSource{Kind: string(e.Kind)}}
			if e.Kind == contextcore.EvidenceVisual {
				visual++
				s.key = "v" + strconv.Itoa(visual)
				s.doc.TimestampMs = e.TimestampMs
			} else {
				audio++
				s.key = "a" + strconv.Itoa(audio)
			}
			s.doc.Source = s.key
			out = append(out, s)
			i = len(out) - 1
			index[id] = i
		}
		doc := &out[i].doc
		switch e.Facet {
		case contextcore.FacetTranscript:
			doc.Transcript, doc.Language = e.Text, e.Language
		case contextcore.FacetFrameDescription:
			doc.Description = e.Text
		case contextcore.FacetDetection:
			doc.Detections = append(doc.Detections, promptDetection{Type: e.Type, Value: e.Value})
		}
	}
	return out
}

// UserPrompt returns the evidence message for group.
func UserPrompt(group contextcore.EvidenceGroup) ([]byte, error) {
	in := promptInput{Window: promptWindow{StartMs: group.Window.StartMs, EndMs: group.Window.EndMs}}
	for _, s := range sources(group) {
		in.Sources = append(in.Sources, s.doc)
	}
	data, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	return append([]byte("Sources:\n"), data...), nil
}

// ResponseSchema returns the JSON schema for group.
func ResponseSchema(group contextcore.EvidenceGroup) (map[string]any, error) {
	var keys []string
	for _, s := range sources(group) {
		keys = append(keys, s.key)
	}
	if len(keys) == 0 {
		keys = []string{"a1"} // schema needs a non-empty enum; unused when evidence empty
	}
	refs := map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "string", "enum": keys}}
	confidence := map[string]any{"type": "number", "minimum": 0, "maximum": 1}
	// Snake_case is enforced in parseAnswer; OpenAI strict json_schema rejects "pattern".
	label := map[string]any{"type": "string", "minLength": 1, "maxLength": 48}
	name := map[string]any{"type": "string", "minLength": 1, "maxLength": 64}
	values := map[string]any{"type": "array", "items": object([]string{"value", "confidence", "sources"}, map[string]any{
		"value": label, "confidence": confidence, "sources": refs,
	})}
	entities := map[string]any{"type": "array", "items": object([]string{"type", "value", "confidence", "sources"}, map[string]any{
		"type": map[string]any{"type": "string", "enum": EntityTypes}, "value": name, "confidence": confidence, "sources": refs,
	})}
	event := object([]string{"entities", "topics", "objects", "brands", "confidence"}, map[string]any{
		"entities":   entities,
		"topics":     values,
		"objects":    values,
		"brands":     values,
		"confidence": confidence,
	})
	return object([]string{"events"}, map[string]any{
		"events": map[string]any{"type": "array", "items": event},
	}), nil
}

func object(required []string, properties map[string]any) map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             required,
		"properties":           properties,
	}
}
