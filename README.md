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

Read in this order:

1. `docs/poc/POC-SPEC.md`
2. `docs/contracts/CONTRACTS-V1.md`
3. `docs/adr/`
4. `specs/`
5. `AGENTS.md`
6. `configs/experiments/`

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
