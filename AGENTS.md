# AGENTS.md

This repository is architecture- and specification-driven.

## Mandatory reading order

Before implementing any milestone:

1. Read `docs/poc/POC-SPEC.md`.
2. Read `docs/contracts/CONTRACTS-V1.md`.
3. Read every Accepted ADR under `docs/adr/`.
4. Read the schemas relevant to the change under `specs/`.
5. Read the experiment configuration relevant to the change.
6. Inspect existing tests before changing implementation.

## Source-of-truth precedence

When documents disagree, stop and surface the conflict. Do not silently choose one.

Expected precedence:

1. Accepted ADRs for architectural decisions.
2. Versioned schemas for wire/domain contracts.
3. Contract specification for semantic rules.
4. POC-SPEC for scope, hypotheses and evaluation intent.
5. Experiment configuration for a specific run.
6. Implementation code.

## Architectural guardrails

- Context Intelligence describes content; Shoppable owns commerce decisions.
- Never add product IDs, SKUs, retailer selection, pricing, inventory, checkout, offers or commercial ranking to the Context Core.
- `Observation != ContextEvent`.
- Audio and visual observations are evidence.
- Context Fusion derives semantic context over time.
- Every ContextEvent must preserve evidence and provenance.
- Evidence must reference its source Observation when available.
- Consumers must not depend on provider-native AI payloads.
- Keep provider/model integrations behind ports/adapters.
- Live and VOD must share the same core contracts wherever practical.
- Do not silently modify schemas to make implementation easier.
- A breaking contract change requires a new schema version.
- A major architectural change requires a new ADR or update to an existing ADR.
- Prefer deterministic orchestration over autonomous agents for POC v1.
- Do not introduce RAG unless a concrete knowledge-enrichment requirement is documented.
- Product retrieval is not a Context Intelligence concern.

## Validation guardrail

Contract validation has two layers:

1. JSON Schema validation: structure, types, required fields, enums and numeric bounds.
2. Domain validation: cross-field and semantic invariants.

Do not force semantic invariants into JSON Schema when they are clearer and safer in Go domain validation.

## Go guidance

- Keep domain types independent from vendor SDK types.
- Prefer small packages with explicit responsibilities.
- Use interfaces at external boundaries.
- Propagate `context.Context` through I/O/provider boundaries.
- Make concurrency bounded and observable.
- Avoid hidden global state.
- Return errors with useful context.
- Keep timestamps and temporal windows explicit.
- Tests must cover mapping, schema validation, domain validation, temporal behavior and experiment evaluation.

## Agent workflow

For each milestone:

1. State which specs/ADRs govern the work.
2. Propose the smallest implementation plan.
3. Implement without expanding scope.
4. Add/update tests.
5. Run `go test ./...`.
6. Report deviations, assumptions and unresolved questions.
7. Do not rewrite architecture documents unless explicitly required.
