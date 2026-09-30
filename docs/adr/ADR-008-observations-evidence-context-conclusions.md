# ADR-008 — Observations Are Immutable Perception Evidence; ContextEvents Are Derived Semantic Conclusions

**Status:** Accepted for POC (M07)

## Context
M05 and M06 produce AudioObservations and VisualObservations. M07 must turn them into ContextEvent v1 without collapsing perception into context (ADR-002), while keeping every conclusion attributable (ADR-003) and every provider behind an adapter (ADR-004, ADR-006). M06 visual confidence is model-verbalized and uncalibrated, whisper.cpp gives no transcript confidence, and ContextEvent v1 nonetheless requires an event-level confidence, an item-level confidence on every entity, topic, object and brand, and a non-blank description on every visual evidence item.

## Decision
- AudioObservation and VisualObservation are immutable perception evidence. Context reasoning consumes them by copy and never cleans, rewrites, normalizes or re-scores them.
- Context reasoning first correlates observations temporally, then applies semantic reasoning, and only then produces ContextEvents. For POC v1 one M04 MediaSegment window is one reasoning unit; an observation belongs to it only when its content and window equal the segment's. There is no cross-window state.
- Reasoning sits behind a vendor-neutral `Reasoner` port (`internal/context`). Adapters return structured candidates that must cite the evidence they rely on. The core rejects any citation of an observation, or a visual frame timestamp, that is not in the reasoning unit. This is the hallucination containment boundary.
- ContextEvents retain references to the observations supporting the conclusion. Evidence text comes from perception only: the transcript verbatim for audio, and for visual evidence the frame description verbatim or, when the frame has none, a deterministic `TYPE: value; …` rendering of that frame's own detections. Reasoner prose never enters evidence.
- Perception confidence remains evidence metadata and is not interpreted as calibrated probability by the Context Engine.
- Event-level and item-level confidences are the reasoner's own stated 0..1 values. They are required (a candidate without them is rejected, never defaulted or clamped), uncalibrated and not probabilities. The Context Engine never averages, combines or derives confidences from perception values.
- ContextEvent IDs are deterministic: `ctx:<segmentId>:<ordinal>`, with the ordinal the 1-based position of the candidate in the reasoner's answer.

## Consequences
Positive:
- provider-independent domain;
- explainable ContextEvents;
- deterministic correlation;
- replay capability;
- model replacement without ContextEvent consumer changes;
- easier evaluation of perception vs reasoning;
- hallucination containment through evidence validation.

Trade-offs:
- additional internal reasoning structures;
- more validation;
- more explicit orchestration;
- potentially higher implementation complexity than direct model-to-ContextEvent mapping;
- context confidences are only as meaningful as the reasoner's self-assessment; evaluation (M08/M09) must treat them as uncalibrated scores;
- one-window reasoning can miss context spanning adjacent windows.

## Rejected alternatives
- `Audio + frames → VLM/LLM → ContextEvent directly`: couples perception, temporal correlation and semantic interpretation and weakens replay/debug traceability.
- Deriving event confidence from perception confidences (average, min, max or other formula): perception values are uncalibrated or absent, and no validated semantics exist.
- A constant confidence or description: fabrication.
- Relaxing ContextEvent v1 (optional confidence or description): a breaking contract revision not justified for POC v1.
