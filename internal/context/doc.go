// Package context is the vendor-neutral multimodal context reasoning core of
// the Context Intelligence pipeline. Importers alias it (for example
// contextcore) because its name matches the standard library context package.
//
// AudioObservation and VisualObservation are immutable perception evidence;
// a ContextEvent is a derived semantic conclusion. The core keeps these apart:
//
//	observations of one MediaSegment
//	  → Correlator (SegmentCorrelator)  → EvidenceGroup
//	  → Reasoner (port)                 → Candidates
//	  → Ground                          → only evidence-backed candidates
//	  → NewContextEvent                 → ContextEvent v1
//
// Correlation happens before reasoning. One MediaSegment window is one
// reasoning unit; there is no cross-window state. Observations are projected
// by copy into internal Evidence values and never modified; blank transcripts
// and frames without a description or detections carry nothing to reason
// about and are left out of the group.
//
// Every candidate must cite evidence that exists in its group, including the
// exact frame timestamp for visual evidence; anything else is rejected. This
// is the hallucination containment boundary. Evidence text in the emitted
// event comes from perception (transcripts, frame descriptions or a rendering
// of the frame's detections), never from the reasoner.
//
// Perception confidence is carried as evidence metadata only. Event and item
// confidences are the reasoner's own stated values, required and uncalibrated;
// this package never averages, combines or derives confidences, and none of
// them is a probability.
//
// The package depends only on pkg/contracts. Reasoner adapters live in
// subpackages and are selected by name through a Registry. No commerce
// reasoning happens here.
package context
