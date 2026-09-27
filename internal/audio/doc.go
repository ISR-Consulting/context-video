// Package audio is the vendor-neutral speech-to-text port of the Context
// Intelligence pipeline.
//
// A [Transcriber] turns the audio of one media window into a [Transcription].
// [NewObservation] maps that transcription onto the contracts.AudioObservation
// evidence contract. Adapters for concrete providers live in subpackages and
// are selected by name through a [Registry], so callers never import or name a
// vendor. Provider-native payloads never cross this package boundary.
//
// Audio observations are evidence, not context: this package creates no
// ContextEvents and performs no fusion.
package audio
