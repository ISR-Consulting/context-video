// Package pipeline composes perception stages into harness.Pipeline
// implementations.
//
// The Audio pipeline cuts each test case into M04 media.Segment windows and
// transcribes every window through a vendor-neutral audio.Transcriber,
// returning AudioObservations only. It performs no fusion and emits no
// ContextEvents; the M03 runner still validates everything it returns.
package pipeline
