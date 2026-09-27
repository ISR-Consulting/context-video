# context-video

Proof of Concept for **Context Video Intelligence**, initially focused on Globoplay Shoppable use cases across Live and VOD/VideoID.

## Purpose

Validate whether audiovisual content can be transformed into structured, evidence-backed, temporally synchronized context events that can later be consumed by Shoppable.

Context Intelligence understands content. It does **not** select products, offers, retailers, prices, inventory or SKUs.

## Core flow

```text
Media (Live / VOD)
  -> Media Ingestion
  -> AudioObservation
  -> VisualObservation
  -> Temporal Context Fusion
  -> ContextEvent v1
  -> Context Timeline / Event Boundary
  -> Shoppable
```

## Core domain rule

```text
Observation != ContextEvent
```

Audio and visual pipelines produce observations. Context Fusion converts temporally aligned observations into a stable semantic context.

## Repository philosophy

This POC is **architecture- and specification-driven**.

Read in this order (the mandatory order defined in `AGENTS.md`, which also
sets source-of-truth precedence and guardrails):

1. `docs/poc/POC-SPEC.md`
2. `docs/contracts/CONTRACTS-V1.md`
3. `docs/adr/`
4. `specs/`
5. `configs/experiments/`
6. existing tests for the area being changed

## Evaluation harness

The M03 harness loads an experiment configuration, validates a golden dataset
through the M02 loader, runs a pipeline over every test case in manifest order,
validates pipeline output against the M01 contracts and persists the canonical
`ExperimentResult`:

```bash
go run ./cmd/harness \
  --config configs/experiments/multimodal-5s.yaml \
  --manifest manifests/<dataset-id>-v<dataset-version>.json \
  --dataset-root dataset \
  --specs-root specs \
  --output results \
  --pipeline validation-only
```

`--manifest` is relative to `--dataset-root`. The result is written to
`<output>/<experiment-id>/<dataset-id>-v<dataset-version>.json`; an existing
file is never overwritten, so move it or choose another `--output` to rerun.
Generated results must not be committed.

`validation-only` is currently the only pipeline. It emits no observations or
ContextEvents and calls no provider: it is an infrastructure smoke run of
configuration, dataset, orchestration and persistence, not an E01–E05 AI
experiment. Unmeasured metrics are left absent.

## Live simulator

The M04 live simulator (`internal/media`, `cmd/live-simulator`) cuts each golden
dataset test case into contiguous `MediaSegment` windows of the experiment's
`window.size` and replays them in media time. Live and VOD share the same
segments and differ only in pacing:

```bash
go run ./cmd/live-simulator \
  --config configs/experiments/multimodal-5s.yaml \
  --manifest manifests/<dataset-id>-v<dataset-version>.json \
  --dataset-root dataset \
  --specs-root specs \
  [--test-case <test-case-id>] \
  [--speed 1.0 | --instant]
```

- Windows run back to back from 0 to the declared `durationMs`; the last window
  is truncated at the duration, never dropped or padded. Segment IDs are
  `<contentId>:<startMs>-<endMs>`; content and `sourceUri` are copied unchanged.
- With `--speed` (default 1, real time) each segment is released once its
  whole window has elapsed, scheduled from the replay start. `--instant` emits
  every segment immediately and its output is byte-identical across runs.
- stdout gets one schema- and domain-validated `MediaSegment` per line (JSON
  Lines); stderr gets a one-line summary per test case. Every test case is
  replayed in manifest order, each from media time 0.
- Exit codes: 0 on success or `-h`, 1 on runtime errors (including Ctrl-C), 2
  on usage errors.

The simulator never opens or decodes media and calls no provider. LOCAL and
FIXTURE media bytes are SHA-256-verified by the M02 loader; CONTROLLED_SOURCE
media is replayed from its declared duration only. It writes no files.

## Language

Go is selected for the POC implementation, Evaluation Harness and Live Simulator. This does **not** establish Go as the mandatory production language.

## Baseline version

This package represents **baseline v0.2**, which refines the first contract baseline with:

- shared `ContentRef`;
- shared `TimeWindow`;
- structured `Transcript`;
- structured visual observations;
- evidence references to source Observation IDs;
- per-item confidence;
- separation between schema validation and domain validation.
