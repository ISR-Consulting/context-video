# POC Implementation Milestones

Implementation agents should work milestone-by-milestone.

## M00 — Architecture baseline
Status: complete.

Deliverables:
- POC-SPEC;
- ADRs;
- Contract Specification;
- JSON Schemas;
- experiment matrix;
- agent guardrails.

## M01 — Contract Types and Validation
Status: complete.

### M01.1 — Domain model review
Confirm:
- ContentRef;
- TimeWindow;
- Transcript;
- Visual structured observations;
- ContextEvent v1;
- evidence references;
- confidence semantics.

### M01.2 — Schema freeze
Validate all JSON Schemas and examples.

After acceptance, ContextEvent v1 is frozen for POC implementation.

### M01.3 — Go contract types
Implement Go representations for:
- MediaSegment;
- AudioObservation;
- VisualObservation;
- ContextEventV1;
- ExperimentResult;
- shared types.

### M01.4 — Validation
Implement:
- JSON Schema validation;
- Go domain validation;
- valid/invalid/boundary tests.

### M01 exit criteria
- all schemas valid;
- examples validate;
- Go types implemented;
- schema validation implemented;
- domain validation implemented;
- valid tests;
- invalid tests;
- boundary tests;
- `go test ./...` passes;
- no AI provider call introduced.

## M02 — Golden Dataset model
Status: complete.

Define manifest and ground-truth loading/evaluation primitives.

## M03 — Evaluation Harness core
Status: complete.

Load experiment configuration, execute a pluggable pipeline and persist experiment results.

## M04 — Live Simulator
Status: complete.

Replay approved VOD media according to media time and emit deterministic segments.

## M05 — Audio observation adapter
Status: complete.

Integrate selected STT provider behind an interface.

## M06 — Visual observation adapter
Status: complete.

Implement frame sampling/scene selection and selected visual provider behind an interface.

## M07 — Context Fusion
Status: complete.

Fuse temporally aligned observations into ContextEvent v1 with evidence and provenance.

## M08 — Experiment execution
Status: complete.

Run E01–E05 against the Golden Dataset. Canonical Mac run:
`2026-10-07-m5-7b-v2` at git `899e7ef` (Live E01–E05 + E04 VOD; v2 prompts;
vision `max-tokens` 1024). Raw artifacts stay outside the repo.

## M09 — Evaluation report
Status: in progress.

Compare quality, latency, stability and cost and record architecture findings.
See [`M09-EVALUATION-REPORT.md`](M09-EVALUATION-REPORT.md).
