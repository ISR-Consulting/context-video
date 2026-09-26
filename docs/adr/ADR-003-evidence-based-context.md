# ADR-003 — Evidence-Based Context

**Status:** Accepted for POC

## Context
A confidence score alone is insufficient for debugging, evaluation and traceability.

## Decision
Every ContextEvent must retain attributable evidence and provenance. Where observations are available, evidence references the originating `observationId`.

## Consequences
- False positives/negatives can be investigated.
- Evaluation can trace output to source signals.
- Replay/debug becomes easier.
- Payload/storage requirements may increase.
