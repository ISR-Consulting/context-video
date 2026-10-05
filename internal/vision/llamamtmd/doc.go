// Package llamamtmd is a vision.Analyzer adapter for llama.cpp's local
// multimodal command-line tool (llama-mtmd-cli) running a GGUF vision-language
// model such as Qwen2.5-VL with its matching mmproj projector.
//
// For each requested frame, ffmpeg extracts one JPEG at the frame timestamp of
// a local media file and llama-mtmd-cli answers a versioned prompt
// ([PromptVersion]) with one compact JSON document constrained by a GBNF
// grammar ([Grammar]) to the VisualObservation detection types, at most
// [MaxDetections] detections and lowercase snake_case values for OBJECT,
// TOPIC, SCENE and ACTION. Later frames of a window are told which ENTITY,
// BRAND and TEXT values an earlier frame already reported ([FramePrompt]), so
// persistent overlays are not repeated in every frame. The answer is read from stdout, parsed
// strictly and mapped onto provider-neutral vision.Frame values. A frame whose
// answer is malformed, uses an unknown type or omits a confidence fails the
// whole request; nothing is repaired or defaulted.
//
// Confidence values are the model's own verbalized 0..1 estimates, passed
// through unchanged. llama-mtmd-cli exposes no token probabilities, so they
// are uncalibrated and must not be treated as probabilities downstream.
//
// Model, projector and binary paths are injected through options; nothing is
// hardcoded. No download flag is ever passed, inherited LLAMA_ARG_* variables
// are removed from the child environment and ffmpeg is restricted to the file
// protocol, so no network access is made. CONTROLLED_SOURCE media is rejected
// with vision.ErrUnsupportedSource.
package llamamtmd
