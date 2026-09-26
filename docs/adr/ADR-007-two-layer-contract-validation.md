# ADR-007 — Two-Layer Contract Validation

**Status:** Accepted for POC

## Context
JSON Schema is well suited to structural validation but is not the best place for every cross-field and semantic invariant.

## Decision
Contract validation is split into two layers:

1. JSON Schema validation for structure, types, required fields, enums and numeric bounds.
2. Go domain validation for temporal relationships, evidence references, content consistency and semantic invariants.

## Consequences
- Schemas remain understandable.
- Semantic rules are testable in Go.
- Validation responsibilities are explicit.
