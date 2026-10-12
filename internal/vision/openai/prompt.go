package openai

import (
	"slices"
	"strconv"
	"strings"

	"github.com/ISR-Consulting/context-video/internal/vision"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// PromptVersion matches the local llama-mtmd vision-frame-v2 grammar and labels.
const PromptVersion = "vision-frame-v2"

// MaxDetections caps the detections of one frame.
const MaxDetections = 12

// Prompt asks the model for one compact JSON document describing a single frame.
const Prompt = `You are a visual perception component. Look only at this single video frame and report what is visibly present.
Answer with exactly one compact JSON object, without spaces or line breaks between its parts: {"description":"...","observations":[{"type":"...","value":"...","confidence":0.9}]}.
"type" must be one of:
- OBJECT: a physical thing (football_jersey, ball, goal_post);
- SCENE: the setting (stadium, studio, kitchen);
- ACTION: what is happening (corner_kick, goal_celebration);
- TOPIC: the subject area of the frame (football);
- ENTITY: a named person, team, organization or place recognizable in the frame, written as shown (Germany, Palmeiras); roles such as goalkeeper, player or man are not entities;
- BRAND: a brand name or logo visibly shown, including sponsors on boards and shirts (Adidas, Volkswagen); a brand is BRAND, never TEXT;
- TEXT: other legible on-screen text that matters for context (score lines, captions, player names), verbatim.
OBJECT, SCENE, ACTION and TOPIC values are short English labels in lowercase snake_case.
Report at most 12 observations, the most informative first, each only once. Do not report running clocks or timers.
"confidence" is your own estimate, from 0 to 1, that the observation is visibly correct.
"description" is one short, neutral sentence about the frame.
Report only what is visible; do not guess. Do not mention products for sale, prices, retailers, offers or recommendations.
If nothing is recognizable, return an empty "observations" list.`

var repeatedTypes = []contracts.VisualDetectionType{contracts.VisualDetectionEntity, contracts.VisualDetectionBrand, contracts.VisualDetectionText}

// FramePrompt returns the prompt for a frame given earlier detections of the window.
func FramePrompt(earlier []vision.Frame) string {
	var seen []string
	for _, f := range earlier {
		for _, d := range f.Detections {
			if !slices.Contains(repeatedTypes, d.Type) {
				continue
			}
			item := string(d.Type) + " " + strconv.Quote(d.Value)
			if !slices.Contains(seen, item) {
				seen = append(seen, item)
			}
		}
	}
	if len(seen) == 0 {
		return Prompt
	}
	return Prompt + "\nAlready reported for an earlier frame of this window; omit these unless they changed: " + strings.Join(seen, "; ") + "."
}

var snakeTypes = []contracts.VisualDetectionType{contracts.VisualDetectionObject, contracts.VisualDetectionTopic, contracts.VisualDetectionScene, contracts.VisualDetectionAction}

func isSnake(s string) bool {
	if s == "" || s[0] == '_' || s[len(s)-1] == '_' || strings.Contains(s, "__") {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' {
			return false
		}
	}
	return true
}

func frameResponseSchema() map[string]any {
	confidence := map[string]any{"type": "number", "minimum": 0, "maximum": 1}
	obs := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"type", "value", "confidence"},
		"properties": map[string]any{
			"type": map[string]any{
				"type": "string",
				"enum": []string{"OBJECT", "ENTITY", "TOPIC", "BRAND", "TEXT", "SCENE", "ACTION"},
			},
			"value":      map[string]any{"type": "string", "minLength": 1, "maxLength": 60},
			"confidence": confidence,
		},
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"description", "observations"},
		"properties": map[string]any{
			"description": map[string]any{"type": "string", "minLength": 1, "maxLength": 160},
			"observations": map[string]any{
				"type":     "array",
				"maxItems": MaxDetections,
				"items":    obs,
			},
		},
	}
}
