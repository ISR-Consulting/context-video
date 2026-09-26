# ADR-004 — Model-Agnostic Contracts

**Status:** Accepted for POC

## Context
Models, providers and prompts may change during experiments.

## Decision
Domain consumers depend on stable, versioned schemas rather than provider-native AI payloads.

## Consequences
- Provider integrations require adapters.
- Provider experiments do not redefine downstream contracts.
- Contract changes become explicit architecture changes.
