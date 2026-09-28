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

`--pipeline` selects one of:

- `validation-only` (default) emits no observations or ContextEvents and calls
  no provider: it is an infrastructure smoke run of configuration, dataset,
  orchestration and persistence, not an E01–E05 AI experiment.
- `audio` (M05) cuts every test case into the M04 `MediaSegment` windows and
  transcribes each window with the STT adapter named by the config's
  `audio.provider`, emitting one `AudioObservation` per window and no
  ContextEvents. It needs an audio-only configuration (`vision.enabled: false`).

Unmeasured metrics are left absent in both.

## Audio observations

`internal/audio` is the vendor-neutral STT port: a `Transcriber` receives a
segment window plus an audio source and returns a provider-neutral
transcription that is mapped onto `AudioObservation`. Observation IDs are
`aud:<segmentId>`. Transcript `confidence` is set only when the provider
supplies one, and provenance records provider, model and `pipelineVersion`
(`poc-v1`). Adapters are registered by name in `internal/audio/providers`; the
harness selects one through `audio.provider` and passes adapter settings
opaquely with repeatable `--audio-option key=value` flags.

The committed configurations keep `audio.provider: TBD`, which is not a
registered adapter, so `--pipeline audio` refuses to run them. Use a copy with
the provider you want.

### whisper.cpp (`whisper-cpp`)

The first adapter runs a local whisper.cpp install. For each window, ffmpeg
(file protocol only) extracts 16 kHz mono WAV and `whisper-cli` transcribes it
to JSON; nothing touches the network. whisper-cli reports no utterance
confidence, so none is emitted. `CONTROLLED_SOURCE` media is rejected with a
clear error; `LOCAL`/`FIXTURE` media is read from `--dataset-root`.

| Option | Default | Meaning |
|---|---|---|
| `model` | required | path to a ggml model file |
| `binary` | `whisper-cli` on `PATH` | whisper.cpp CLI |
| `ffmpeg` | `ffmpeg` on `PATH` | extraction tool |
| `language` | `auto` | whisper `-l`, e.g. `pt` |
| `threads` | whisper default | whisper `-t` |

On macOS with Homebrew:

```bash
brew install whisper-cpp ffmpeg
# download a model, e.g. ggml-large-v3-turbo.bin, into ~/models
sed '/^audio:/,/^vision:/ s/provider: TBD/provider: whisper-cpp/' \
  configs/experiments/audio-only-5s.yaml > /tmp/e01-whisper.yaml
go run ./cmd/harness \
  --config /tmp/e01-whisper.yaml \
  --manifest manifests/<dataset-id>-v<dataset-version>.json \
  --dataset-root dataset \
  --output /tmp/results \
  --pipeline audio \
  --audio-option model="$HOME/models/ggml-large-v3-turbo.bin" \
  --audio-option language=pt
```

The `sed` rewrites only `audio.provider`. The dataset must reference approved
`LOCAL` media. Offline tests use fakes; an
opt-in integration test runs the real tools:

```bash
CONTEXT_VIDEO_WHISPER_MODEL=$HOME/models/ggml-large-v3-turbo.bin \
CONTEXT_VIDEO_WHISPER_LANGUAGE=pt \
go test -tags integration -run Integration -v ./internal/audio/whispercpp
```

Set `CONTEXT_VIDEO_WHISPER_AUDIO` to a local speech file to transcribe real
speech instead of generated silence.

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
