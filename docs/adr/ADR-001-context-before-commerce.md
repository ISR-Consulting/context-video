# ADR-001 — Context Before Commerce

**Status:** Accepted for POC

## Context
Context Intelligence identifies semantic content while Shoppable owns product retrieval, matching, ranking and other commercial decisions.

## Decision
Context Intelligence will not emit product IDs, SKUs, retailers, prices, inventory, offers or commercial rankings.

## Consequences
- Context contracts remain reusable beyond Shoppable.
- Commerce can evolve independently.
- Product matching remains outside POC v1.
