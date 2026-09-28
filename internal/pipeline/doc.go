// Package pipeline composes perception stages into harness.Pipeline
// implementations.
//
// The Audio pipeline cuts each test case into M04 media.Segment windows and
// transcribes every window through a vendor-neutral audio.Transcriber,
// returning AudioObservations only. The Vision pipeline cuts the same windows,
// samples frames in each according to vision.sampling and analyzes them
// through a vendor-neutral vision.Analyzer, returning VisualObservations only.
// Neither performs fusion or emits ContextEvents; the M03 runner still
// validates everything they return.
package pipeline
