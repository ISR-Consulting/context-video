// Package llamacpp is a context Reasoner adapter for llama.cpp's local
// non-interactive command-line tool (llama-completion) running a GGUF
// instruction-tuned text model.
//
// For each EvidenceGroup it writes the versioned system prompt
// ([PromptVersion]), the group's evidence as compact JSON sources and a
// per-group JSON schema to a scratch directory, then runs one single-turn chat
// (-cnv -st) at temperature 0 with a fixed seed. Sources are keyed a1, a2, ...
// (transcripts) and v1, v2, ... (frames) and carry neither observation IDs nor
// perception confidences. The schema restricts entity types to [EntityTypes],
// topic, object and brand labels to lowercase snake_case, and every item's
// "sources" to the group's own keys. The answer is read from stdout, parsed
// strictly and mapped onto provider-neutral Candidates whose evidence is the
// union of their items' sources; the core grounds them again. A malformed
// answer fails the group; nothing is repaired or defaulted.
//
// Every confidence in an answer is the model's own verbalized 0..1 estimate,
// item by item. llama-completion exposes no calibrated scores, so these values
// are uncalibrated and must not be treated as probabilities downstream.
//
// Model and binary paths are injected through options; nothing is hardcoded.
// --offline is passed, no download flag is ever passed and inherited
// LLAMA_ARG_* variables are removed from the child environment, so no network
// access is made. Output is reproducible for a fixed llama.cpp build, model,
// thread count and backend; bit-identity across them is not guaranteed.
package llamacpp
