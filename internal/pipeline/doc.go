// Package pipeline composes perception stages into harness.Pipeline
// implementations.
//
// The Audio pipeline cuts each test case into M04 media.Segment windows and
// transcribes every window through a vendor-neutral audio.Transcriber,
// returning AudioObservations only. The Vision pipeline cuts the same windows,
// samples frames in each according to vision.sampling and analyzes them
// through a vendor-neutral vision.Analyzer, returning VisualObservations only.
// Neither performs fusion or emits ContextEvents.
//
// The Multimodal pipeline cuts the same windows and, for each one in media
// order, runs the enabled perception ports (audio, vision or both), then the
// vendor-neutral internal/context engine: temporal correlation, semantic
// reasoning, evidence grounding and ContextEvent v1 mapping. It returns the
// observations together with the ContextEvents derived from them. It reuses
// the M05/M06 ports, observation mappers and resolvers without changing them.
//
// The M03 runner validates everything every pipeline returns.
package pipeline
