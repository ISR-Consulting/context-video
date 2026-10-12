// Package openai is a vision.Analyzer adapter for OpenAI Chat Completions
// vision (cheap default: gpt-4o-mini with image detail low).
//
// For each requested frame, ffmpeg extracts one JPEG from a local media file
// and the adapter sends it with the versioned vision-frame-v2 prompt. The
// model's JSON answer is parsed strictly onto provider-neutral vision.Frame
// values; native OpenAI payloads never leave this package. Credentials come
// from options or OPENAI_API_KEY. CONTROLLED_SOURCE media is rejected with
// vision.ErrUnsupportedSource.
package openai
