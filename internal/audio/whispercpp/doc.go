// Package whispercpp is an audio.Transcriber adapter for the local whisper.cpp
// command-line tool (whisper-cli).
//
// For each request, ffmpeg extracts exactly the requested window of a local
// media file to 16 kHz mono WAV, whisper-cli transcribes it to a JSON file and
// the adapter maps that JSON onto a provider-neutral audio.Transcription.
// Binary and model paths are injected through options; nothing is hardcoded.
// ffmpeg is restricted to the file protocol and no network access is made.
// CONTROLLED_SOURCE media is rejected with audio.ErrUnsupportedSource.
//
// whisper-cli JSON output carries no utterance-level confidence, so the
// transcription confidence is always left absent.
package whispercpp
