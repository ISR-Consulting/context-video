// Package openai is an audio.Transcriber adapter for OpenAI's Audio
// Transcriptions API (cheap default: gpt-4o-mini-transcribe).
//
// For each request, ffmpeg extracts exactly the requested window of a local
// media file to 16 kHz mono WAV and the adapter POSTs that file to
// /v1/audio/transcriptions. The JSON response is mapped onto a
// provider-neutral audio.Transcription; native OpenAI payloads never leave
// this package. Binary paths and credentials are injected through options or
// OPENAI_API_KEY; nothing is hardcoded. ffmpeg is restricted to the file
// protocol. CONTROLLED_SOURCE media is rejected with audio.ErrUnsupportedSource.
//
// Confidence is left absent unless the API supplies an explicit 0..1 field.
package openai
