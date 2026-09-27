# Golden Dataset

This directory contains versioned manifests and human-authored ground truth for
repeatable Context Intelligence evaluation. The dataset describes expected
context and media identity; it does not contain generated ContextEvents or
evaluation scores.

```text
dataset/
  manifests/
    <dataset-id>-v<dataset-version>.json
  ground-truth/
    <dataset-id>/
      <test-case-id>.json
  media/
    <approved local media only>
```

Dataset IDs and test-case IDs use lowercase kebab-case. Normalize dataset
versions for filenames. Manifest and ground-truth files are UTF-8 JSON with a
stable source order. A manifest's `groundTruthPath` is relative to this
directory, uses `/` separators, and cannot be absolute or contain escaping
`..` segments.

The v1 schemas are:

- `specs/golden-dataset-manifest-v1.schema.json`
- `specs/golden-ground-truth-v1.schema.json`

Ground truth groups labels into `required`, `optional`, and `forbidden` sets.
It carries no model confidence. Optional alternatives require an
`ambiguityNote`; matching and scoring policy belong to M03.

## Media policy

Media references have a kind, URI, SHA-256 digest, and duration:

- `LOCAL` points to approved media supplied inside the injected dataset
  filesystem.
- `FIXTURE` points to deterministic test bytes inside that filesystem.
- `CONTROLLED_SOURCE` retains an opaque approved-source descriptor and is never
  fetched by the M02 loader.

`dataset/media/` ignores all media by default. Do not commit audiovisual media
without explicit repository-storage authorization and a targeted ignore
exception. The loader performs no downloads and never opens host paths; local
and fixture references are resolved only through its supplied `fs.FS`.

## Loading

`internal/evaluation/dataset.NewLoader` accepts separate dataset and schema
filesystems. `LoadDataset` validates the manifest and ground truths, preserves
their source order, checks cross-file identity and temporal bounds, and verifies
local or fixture media bytes against their declared digest.
