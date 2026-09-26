# ADR-005 — Shared Context Core for Live and VOD

**Status:** Accepted for POC

## Context
Live and VOD differ primarily in ingestion and timing rather than semantic context representation.

## Decision
Use common Observation and ContextEvent contracts for Live and VOD while keeping ingestion strategy separate.

## Consequences
- Live builds context progressively.
- VOD can precompute context timelines.
- Fusion and evaluation logic remain reusable.
