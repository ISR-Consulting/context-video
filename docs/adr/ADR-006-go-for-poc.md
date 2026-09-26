# ADR-006 — Go as the POC Implementation Language

**Status:** Accepted for POC

## Context
The POC requires a reproducible experiment harness, concurrent media/event processing and integration with external AI APIs.

## Decision
Use Go for the Evaluation Harness, Live Simulator and Context Pipeline in POC v1.

## Scope limitation
This decision applies to the POC. It does not establish Go as the mandatory production language.

## Consequences
- Domain contracts remain language-agnostic.
- Vendor SDK types must not leak into domain contracts.
- External AI capabilities are exposed through adapters/ports.
