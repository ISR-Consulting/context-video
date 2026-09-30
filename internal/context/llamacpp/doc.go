// Package llamacpp is a context Reasoner adapter for llama.cpp's local
// non-interactive command-line tool (llama-completion) running a GGUF
// instruction-tuned text model.
//
// For each EvidenceGroup it writes the versioned system prompt
// ([PromptVersion]), the group's evidence as compact JSON and a per-group JSON
// schema to a scratch directory, then runs one single-turn chat (-cnv -st) at
// temperature 0 with a fixed seed. The schema constrains generation to the
// answer shape and to the group's own observation IDs and frame timestamps.
// The answer is read from stdout, parsed strictly and mapped onto
// provider-neutral Candidates; the core grounds them again. A malformed answer
// fails the group; nothing is repaired or defaulted.
//
// Every confidence in an answer is the model's own verbalized 0..1 estimate.
// llama-completion exposes no calibrated scores, so these values are
// uncalibrated and must not be treated as probabilities downstream.
//
// Model and binary paths are injected through options; nothing is hardcoded.
// --offline is passed, no download flag is ever passed and inherited
// LLAMA_ARG_* variables are removed from the child environment, so no network
// access is made. Output is reproducible for a fixed llama.cpp build, model,
// thread count and backend; bit-identity across them is not guaranteed.
package llamacpp
