package llamamtmd

import (
	"slices"
	"strconv"
	"strings"

	"github.com/ISR-Consulting/context-video/internal/vision"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// PromptVersion identifies Prompt, FramePrompt and Grammar. The observation
// provenance contract has no prompt field, so it is recorded here and in the
// README only; change it whenever any of them changes.
const PromptVersion = "vision-frame-v2"

// MaxDetections caps the detections of one frame; Grammar enforces it.
const MaxDetections = 12

// Prompt asks the model for one compact JSON document describing a single
// frame. FramePrompt extends it for later frames of the same window.
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

// repeatedTypes are the detection types that tend to stay on screen across
// frames (overlays, boards, names). Later frames of a window are told which
// of them were already reported.
var repeatedTypes = []contracts.VisualDetectionType{contracts.VisualDetectionEntity, contracts.VisualDetectionBrand, contracts.VisualDetectionText}

// FramePrompt returns the prompt for a frame given the detections of the
// earlier frames of the same window. ENTITY, BRAND and TEXT values already
// reported are listed so the model omits them unless they changed; each
// frame still reports what it sees, and the window's first frame keeps the
// full list.
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

// Grammar is passed to --grammar. Unlike a JSON schema, which llama.cpp turns
// into a grammar that allows indentation and line breaks, it admits only
// compact JSON, so no tokens are spent on whitespace. It also enforces the
// contract detection types, at most MaxDetections detections, lowercase
// snake_case values for OBJECT, TOPIC, SCENE and ACTION, bounded string
// lengths and a confidence in [0, 1].
const Grammar = `root ::= "{\"description\":\"" desc "\",\"observations\":[" ( obs ( "," obs ){0,11} )? "]}"
desc ::= char{1,160}
obs ::= "{\"type\":\"" ( snaketype "\",\"value\":\"" snake | freetype "\",\"value\":\"" free ) "\",\"confidence\":" conf "}"
snaketype ::= "OBJECT" | "TOPIC" | "SCENE" | "ACTION"
freetype ::= "ENTITY" | "BRAND" | "TEXT"
snake ::= [a-z0-9]{1,24} ( "_" [a-z0-9]{1,24} ){0,5}
free ::= char{1,60}
char ::= [^"\\\x00-\x1F] | "\\" ["\\/nt]
conf ::= "0" ( "." [0-9] [0-9]? )? | "1" ( ".0" )?
`

// snakeTypes are the detection types whose values Grammar restricts to
// lowercase snake_case.
var snakeTypes = []contracts.VisualDetectionType{contracts.VisualDetectionObject, contracts.VisualDetectionTopic, contracts.VisualDetectionScene, contracts.VisualDetectionAction}

// isSnake reports whether s is lowercase ASCII snake_case: [a-z0-9]+ words
// joined by single underscores.
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
