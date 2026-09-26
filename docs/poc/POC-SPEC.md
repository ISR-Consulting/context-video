# Context Video Intelligence — POC-SPEC v0.2

**Status:** Draft  
**Domain:** Globoplay / Shoppable  
**Type:** Proof of Concept Specification

## 1. Context

Shoppable needs context relevant to moments in Globoplay content, including VOD/VideoID and Live.

Context Intelligence must identify what is happening in a given temporal interval from audiovisual signals and expose a structured, synchronized and traceable representation.

Signals may include:

- audio;
- image;
- editorial/content metadata when available.

Context Intelligence ends at context generation. Product selection and commercial decisions belong to Shoppable.

## 2. Problem statement

The capability must continuously answer:

> What is happening in this content at this moment?

For Live, the resulting context must become available with sufficiently low latency to remain useful to downstream experiences.

## 3. POC objective

Validate whether a multimodal AI pipeline can transform audiovisual media into a timeline of Context Events using audio and visual evidence.

```text
Media
 -> Perception
 -> Observations
 -> Temporal Context Fusion
 -> ContextEvent
```

Live builds the timeline progressively. VOD may build it ahead of playback. Both should share the same Context Intelligence Core wherever practical.

## 4. Main question

Is it technically viable to identify semantically relevant context in Live and VOD using multimodal signals and expose it as structured, traceable and temporally synchronized Context Events with quality, latency and cost compatible with future Shoppable usage?

## 5. Goals

Validate:

- audiovisual signal extraction;
- STT with timestamps;
- visual evidence extraction;
- audio + image + metadata fusion;
- entity, topic and object identification;
- temporal state/context handling;
- ContextEvent v1 generation;
- evidence and provenance;
- Live processing;
- VOD/VideoID processing;
- end-to-end latency;
- context quality;
- temporal stability;
- estimated cost per processed video hour.

## 6. Non-goals

POC v1 does not include:

- product recommendation;
- retailer integration;
- price or inventory;
- commercial ranking;
- checkout;
- user personalization;
- final TV/mobile/web UX;
- production HA/DR/autoscaling;
- definitive production topology;
- definitive provider/model selection;
- model training/fine-tuning;
- autonomous agents.

## 7. Architectural principles

### P1 — Context before commerce
Context Intelligence describes content. Shoppable decides what to commercialize.

### P2 — Observation is not context
STT and vision create observations. Context Fusion derives semantic context from observations over time.

### P3 — Evidence-based inference
Every ContextEvent must preserve attributable evidence and provenance.

### P4 — Model agnostic
Consumers depend on versioned contracts, not provider-native responses.

### P5 — Shared core
Live and VOD should share observation, fusion and output contracts wherever practical.

### P6 — Measurable by design
Each experiment records input, configuration, output, latency, evaluation, cost estimate and evidence.

## 8. Hypotheses

### H1 — Context extraction
Useful semantic context can be generated from audiovisual content.

### H2 — Multimodality
Audio + image produces better context quality than audio-only or vision-only.

### H3 — Temporal window
A practical window size exists that balances latency, quality and stability.

Initial candidates: **2s, 5s, 10s**.

### H4 — Shared Context Core
Live and VOD can share observation, fusion and Context Event models while differing mainly in ingestion/execution strategy.

### H5 — Operational viability
The pipeline can operate within acceptable latency and cost-per-video-hour constraints.

Product thresholds remain TBD.

## 9. Minimum scenarios

1. Football / sports apparel.
2. Predominantly visual context.
3. Predominantly auditory context.
4. Multimodal context where audio or image alone is ambiguous.

## 10. Live simulation

POC v1 should replay known media according to real time, allowing repeatable Live-like experiments against identical input.

## 11. Latency

**Context Availability Latency**

```text
T(context event available) - T(content occurrence)
```

Measure P50, P95 and P99 end-to-end and stage-level latency.

Semantic latency may dominate compute latency because sufficient temporal evidence may be required before a useful inference is possible.

## 12. Evaluation dimensions

### Quality
Evaluate entities, topics and objects separately.

### Latency
Measure end-to-end and stage-level latency, especially for Live.

### Stability
Measure context churn/persistence to detect unnecessary semantic oscillation.

### Cost
Normalize as cost per processed video hour.

### Traceability
Measure whether emitted Context Events contain attributable source evidence.

## 13. Initial success criteria

| Criterion | Target |
|---|---:|
| Context quality | TBD |
| P95 Live latency | TBD |
| Context stability | TBD |
| Cost / video hour | TBD |
| Schema compliance | 100% |
| Evidence traceability | 100% |
| Shared Live/VOD contract | Required |

TBD values must not be treated as implicit SLAs.

## 14. Initial experiment matrix

| ID | Audio | Vision | Window |
|---|---|---|---:|
| E01 | yes | no | 5s |
| E02 | no | yes | 5s |
| E03 | yes | yes | 2s |
| E04 | yes | yes | 5s |
| E05 | yes | yes | 10s |

## 15. Mandatory outputs

- Golden Dataset;
- Contract Specs;
- Evaluation Harness;
- experiment configurations;
- raw results;
- evaluation report;
- latency analysis;
- cost analysis;
- architecture findings.

## 16. Open questions

1. What is the maximum useful Live latency for Shoppable?
2. What context granularity is required?
3. Are there existing editorial taxonomies that should be reused?
4. Which content metadata is available?
5. Which media source is available to the POC?
6. Which content may be used as Golden Dataset?
7. Is logo/brand recognition required?
8. Is OCR required?
9. Are there restrictions on sending content to external model providers?
10. Could Context Intelligence later serve consumers beyond Shoppable?
