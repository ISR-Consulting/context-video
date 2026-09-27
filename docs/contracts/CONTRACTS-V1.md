# Contract Specification — v1

**Status:** Accepted and frozen for POC v1 after M01
**Purpose:** Define the semantic meaning of Context Video contracts before Go implementation.

## 1. Contract graph

```text
MediaSegment
     │
     ├──────────────┐
     ▼              ▼
AudioObservation  VisualObservation
     │              │
     └──────┬───────┘
            ▼
      Context Fusion
            │
            ▼
      ContextEventV1
            │
            ▼
     ExperimentResult
```

## 2. Shared concepts

### ContentRef

Represents the content being analyzed.

```json
{
  "contentId": "live-xyz",
  "contentType": "LIVE"
}
```

`contentType` is one of:

- `LIVE`
- `VOD`

### TimeWindow

Represents a temporal interval in media time.

```json
{
  "startMs": 142000,
  "endMs": 147000
}
```

Domain invariants:

- `startMs >= 0`
- `endMs > startMs`

The `endMs > startMs` rule is a domain validation rule.

### Provenance

Records how an observation or context was produced.

Provider/model fields are descriptive and must not leak provider-native response types into domain contracts.

## 3. AudioObservation

AudioObservation is source evidence, not semantic context.

```json
{
  "observationId": "aud-01",
  "content": {
    "contentId": "live-xyz",
    "contentType": "LIVE"
  },
  "window": {
    "startMs": 142000,
    "endMs": 147000
  },
  "transcript": {
    "text": "essa é a nova camisa do Palmeiras",
    "language": "pt-BR",
    "confidence": 0.96
  },
  "provenance": {
    "provider": "TBD",
    "model": "TBD",
    "pipelineVersion": "poc-v1"
  }
}
```

## 4. VisualObservation

VisualObservation contains structured observations attached to sampled frames.

```json
{
  "observationId": "vis-01",
  "content": {
    "contentId": "live-xyz",
    "contentType": "LIVE"
  },
  "window": {
    "startMs": 142000,
    "endMs": 147000
  },
  "frames": [
    {
      "timestampMs": 144200,
      "observations": [
        {
          "type": "OBJECT",
          "value": "football_jersey",
          "confidence": 0.91
        },
        {
          "type": "ENTITY",
          "value": "Palmeiras",
          "confidence": 0.87
        }
      ]
    }
  ],
  "provenance": {
    "provider": "TBD",
    "model": "TBD",
    "pipelineVersion": "poc-v1"
  }
}
```

Allowed initial observation types:

- `OBJECT`
- `ENTITY`
- `TOPIC`
- `BRAND`
- `TEXT`
- `SCENE`
- `ACTION`

The list may evolve only through an explicit contract revision.

## 5. ContextEvent v1

ContextEvent is a semantic interpretation over a temporal window.

```json
{
  "eventId": "ctx-983472",
  "schemaVersion": "1.0",
  "content": {
    "contentId": "live-xyz",
    "contentType": "LIVE"
  },
  "window": {
    "startMs": 142000,
    "endMs": 147000
  },
  "context": {
    "entities": [
      {
        "type": "SPORTS_TEAM",
        "value": "Palmeiras",
        "confidence": 0.96
      }
    ],
    "topics": [
      {
        "value": "football",
        "confidence": 0.98
      },
      {
        "value": "sports_apparel",
        "confidence": 0.91
      }
    ],
    "objects": [
      {
        "value": "football_jersey",
        "confidence": 0.94
      }
    ],
    "brands": []
  },
  "confidence": 0.94,
  "evidence": {
    "audio": [
      {
        "observationId": "aud-01",
        "startMs": 142000,
        "endMs": 147000,
        "text": "essa é a nova camisa do Palmeiras"
      }
    ],
    "visual": [
      {
        "observationId": "vis-01",
        "timestampMs": 144200,
        "description": "Palmeiras football jersey visible"
      }
    ]
  },
  "provenance": {
    "pipelineVersion": "poc-v1",
    "fusionProvider": "TBD",
    "fusionModel": "TBD",
    "promptVersion": "context-fusion-v1"
  }
}
```

### Confidence semantics

Confidence appears at two levels:

- item-level confidence: confidence in one entity/topic/object/brand;
- event-level confidence: confidence in the overall ContextEvent interpretation.

The POC must not assume event-level confidence is a simple arithmetic average.

## 6. Evidence rules

Evidence is part of the domain contract.

Where source observations exist, evidence must reference the originating `observationId`.

Domain validation should verify:

- referenced observation exists in the current evaluation context;
- audio evidence interval overlaps the ContextEvent window;
- visual evidence timestamp belongs to the ContextEvent window;
- content identity is consistent across observation and event.

## 7. Validation layers

### Layer 1 — JSON Schema validation

Use JSON Schema for:

- required fields;
- JSON types;
- enums;
- numeric bounds such as confidence `0..1`;
- structural constraints;
- known properties.

### Layer 2 — Domain validation

Use Go domain validation for:

- `endMs > startMs`;
- evidence timestamps/intervals within or overlapping the event window;
- evidence references to known Observation IDs;
- content identity consistency;
- semantic minimum evidence rules;
- future taxonomy/business invariants.

## 8. Explicit exclusions

No Context contract may include:

- productId;
- SKU;
- retailer;
- price;
- inventory;
- offer;
- commercial ranking;
- checkout information.

## 9. M01 freeze rule

After M01 is accepted, `ContextEvent v1` becomes frozen for POC experiment implementation.

Breaking changes require:

- explicit contract review;
- schema version change;
- corresponding ADR/spec update.
