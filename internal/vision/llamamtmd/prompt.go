package llamamtmd

// PromptVersion identifies Prompt and ResponseSchema. The observation
// provenance contract has no prompt field, so it is recorded here and in the
// README only; change it whenever either text changes.
const PromptVersion = "vision-frame-v1"

// Prompt asks the model for one JSON document describing a single frame.
const Prompt = `You are a visual perception component. Look only at this single video frame and report what is visibly present.
Answer with exactly one JSON object: {"description": string, "observations": [{"type": string, "value": string, "confidence": number}]}.
"type" must be one of:
- OBJECT: a physical thing, value in lowercase snake_case (e.g. football_jersey);
- ENTITY: a named person, team or organization recognizable in the frame (e.g. Palmeiras);
- TOPIC: the subject area of the frame (e.g. football);
- BRAND: a brand name or logo visibly shown;
- TEXT: legible on-screen text, verbatim;
- SCENE: the setting (e.g. stadium);
- ACTION: what is happening (e.g. goal_celebration).
"confidence" is your own estimate, from 0 to 1, that the observation is visibly correct.
"description" is one short, neutral sentence about the frame.
Report only what is visible; do not guess. Do not mention products for sale, prices, retailers, offers or recommendations.
If nothing is recognizable, return an empty "observations" list.`

// ResponseSchema is passed to --json-schema so generation is grammar-constrained
// to the detection types of the VisualObservation contract.
const ResponseSchema = `{"type":"object","additionalProperties":false,"required":["description","observations"],` +
	`"properties":{"description":{"type":"string"},"observations":{"type":"array","maxItems":24,"items":{` +
	`"type":"object","additionalProperties":false,"required":["type","value","confidence"],"properties":{` +
	`"type":{"type":"string","enum":["OBJECT","ENTITY","TOPIC","BRAND","TEXT","SCENE","ACTION"]},` +
	`"value":{"type":"string","minLength":1},` +
	`"confidence":{"type":"number","minimum":0,"maximum":1}}}}}}`
