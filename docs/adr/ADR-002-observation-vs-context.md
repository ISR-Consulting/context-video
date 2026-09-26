# ADR-002 — Observation Is Not Context

**Status:** Accepted for POC

## Context
STT and visual analysis produce partial evidence that can fluctuate across short windows.

## Decision
`AudioObservation` and `VisualObservation` are distinct from `ContextEvent`. Context Fusion derives temporally meaningful context from observations.

## Consequences
- Perception and semantic reasoning remain independently testable.
- Raw observations can be inspected.
- Temporal stabilization can evolve independently.
