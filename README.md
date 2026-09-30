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
  -> Temporal correlation + semantic reasoning (Context Fusion)
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
- `vision` (M06) cuts the same windows, samples frames in each according to
  `vision.sampling` and analyzes them with the adapter named by
  `vision.provider`, emitting one `VisualObservation` per window and no
  ContextEvents. It needs a vision-only configuration (`audio.enabled: false`).
- `multimodal` (M07) runs, for every window, the enabled perception adapters
  (audio, vision or both), then the context reasoning core, and emits the
  observations together with the ContextEvents derived from them. It accepts
  audio-only, vision-only and audio + vision configurations. See
  [Multimodal context reasoning](#multimodal-context-reasoning).

Unmeasured metrics are left absent in all of them.

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

## Visual observations

`internal/vision` is the vendor-neutral visual port. A `Sampler` picks frame
timestamps inside each window, an `Analyzer` returns provider-neutral
detections for those frames, and the result is mapped onto
`VisualObservation` with ID `vis:<segmentId>`, one frame entry per sampled
timestamp and provenance (provider, model file name, `pipelineVersion`).
Detections must use the contract types `OBJECT`, `ENTITY`, `TOPIC`, `BRAND`,
`TEXT`, `SCENE` or `ACTION`; anything else is rejected rather than dropped.
Adapters are registered in `internal/vision/providers`; the harness selects one
through `vision.provider` and passes settings with repeatable
`--vision-option key=value` flags.

`vision.sampling` accepts `uniform:N` (N = 1..16): N frames at the centres of N
equal slices of `[startMs, endMs)`, so every frame is strictly inside its
window. For example `uniform:2` on `0-5000` samples 1250 and 3750 ms.
Scene-change selection is not implemented. The committed configurations keep
`vision.provider: TBD` and `vision.sampling: TBD`, which `--pipeline vision`
refuses to run; use a copy.

### llama.cpp multimodal (`llama-mtmd`)

The first adapter runs a local GGUF vision-language model (for example
Qwen2.5-VL) through `llama-mtmd-cli`. For each frame, ffmpeg (file protocol
only) extracts one JPEG and `llama-mtmd-cli` answers the versioned prompt
`vision-frame-v1` (`internal/vision/llamamtmd/prompt.go`) with JSON constrained
by `--json-schema` to the contract types, at temperature 0 and a fixed seed.
The answer is parsed strictly: unknown fields or types, blank values, a
missing or out-of-range confidence, or anything that is not exactly one JSON
object fail the run with the segment, frame and an excerpt of the output.
`CONTROLLED_SOURCE` media is rejected. No download flag is passed and
inherited `LLAMA_ARG_*` variables are removed, so nothing touches the network.

**Confidence is the model's own verbalized 0..1 estimate**, passed through
unchanged. `llama-mtmd-cli` exposes no token probabilities, so these values are
uncalibrated; M07 fusion and M09 evaluation must not treat them as
probabilities. The prompt version is recorded here and in code only, because
the observation provenance contract has no prompt field.

| Option | Default | Meaning |
|---|---|---|
| `model` | required | GGUF vision-language model |
| `mmproj` | required | matching GGUF multimodal projector |
| `binary` | `llama-mtmd-cli` on `PATH` | llama.cpp multimodal CLI |
| `ffmpeg` | `ffmpeg` on `PATH` | frame extraction |
| `threads` | llama.cpp default | `-t` |
| `gpu-layers` | llama.cpp default | `-ngl` |
| `max-tokens` | `512` | answer token limit (`-n`) |
| `max-edge` | `768` | longest frame edge in pixels |

On macOS with Homebrew:

```bash
brew install llama.cpp ffmpeg   # provides llama-mtmd-cli
# Download a model and its projector manually, e.g. from Hugging Face
# ggml-org/Qwen2.5-VL-7B-Instruct-GGUF:
#   Qwen2.5-VL-7B-Instruct-Q4_K_M.gguf and mmproj-Qwen2.5-VL-7B-Instruct-f16.gguf
# (ggml-org/Qwen2.5-VL-3B-Instruct-GGUF is the smaller alternative).
sed -e '/^vision:/,/^fusion:/ s/provider: TBD/provider: llama-mtmd/' \
    -e 's/sampling: TBD/sampling: uniform:2/' \
  configs/experiments/vision-only-5s.yaml > /tmp/e02-llama.yaml
go run ./cmd/harness \
  --config /tmp/e02-llama.yaml \
  --manifest manifests/<dataset-id>-v<dataset-version>.json \
  --dataset-root dataset \
  --output /tmp/results \
  --pipeline vision \
  --vision-option model="$HOME/models/Qwen2.5-VL-7B-Instruct-Q4_K_M.gguf" \
  --vision-option mmproj="$HOME/models/mmproj-Qwen2.5-VL-7B-Instruct-f16.gguf"
```

Memory: the 7B Q4_K_M model plus its f16 projector needs roughly 8 GB of free
RAM (a 16 GB Mac is comfortable); the 3B variant needs roughly 4 GB. Homebrew
builds use Metal on Apple Silicon. The model is reloaded for every frame, so
expect several seconds per frame; latency is measured in M08. The dataset must
reference approved `LOCAL` video. An opt-in integration test runs the real
tools (a generated test pattern is used unless `CONTEXT_VIDEO_VLM_VIDEO` points
to a local video):

```bash
CONTEXT_VIDEO_VLM_MODEL=$HOME/models/Qwen2.5-VL-7B-Instruct-Q4_K_M.gguf \
CONTEXT_VIDEO_VLM_MMPROJ=$HOME/models/mmproj-Qwen2.5-VL-7B-Instruct-f16.gguf \
go test -tags integration -run Integration -v ./internal/vision/llamamtmd
```

## Multimodal context reasoning

`internal/context` is the vendor-neutral reasoning core
([ADR-008](docs/adr/ADR-008-observations-evidence-context-conclusions.md),
[M07 spec](docs/poc/M07-CONTEXT-REASONING-OVERRIDE.md)). Observations are
immutable evidence; a ContextEvent is a conclusion drawn from them:

```text
MediaSegment
  -> AudioObservation (M05) / VisualObservation (M06)
  -> temporal correlation      one segment window = one EvidenceGroup
  -> semantic reasoning        Reasoner port -> candidates citing evidence
  -> grounding                 unknown observations or frame times rejected
  -> ContextEvent v1           ctx:<segmentId>:<n>
```

- **Correlation.** An observation joins a segment's group only when its
  content and window equal the segment's; frames must lie inside the window.
  Evidence is ordered by time, then modality (audio first), observation ID and
  original item order. Blank transcripts and frames with neither a description
  nor detections carry nothing to reason about and are not citable. Nothing is
  rewritten: `[BLANK_AUDIO]`-style markers stay verbatim.
- **Grounding.** Every candidate must cite at least one observation in its
  group, and a visual citation must name a frame timestamp that observation
  actually has. Anything else fails the segment, and with it the run.
- **Evidence text** comes from perception only: the transcript for audio, and
  the frame description or, without one, the frame's detections rendered as
  `TYPE: value; …` for visual evidence.
- **Confidence.** Event and item confidences are the reasoner's own stated
  0..1 values: required, uncalibrated and **not probabilities**. The core never
  averages or derives confidences, and perception confidences are passed to the
  reasoner only as labelled, uncalibrated metadata.
- **Provenance.** `fusionProvider` is the reasoner name, `fusionModel` the
  model file name, `promptVersion` the reasoning prompt version.
- **Output.** All observations and ContextEvents stay in the in-memory run
  outcome; the persisted `ExperimentResult` remains the metrics envelope.
  Correlation, ordering, IDs and mapping are deterministic; model output is
  reproducible only for a fixed llama.cpp build, model, thread count and
  backend.

The reasoner is selected by the existing `fusion.provider` and configured with
repeatable `--context-option key=value` flags. `--audio-option` and
`--vision-option` apply to the multimodal pipeline as well. The committed
configurations keep `fusion.provider: TBD`; use a copy.

### llama.cpp text model (`llama-cpp`)

The first reasoner runs a local GGUF instruction-tuned text model through
llama.cpp's non-interactive `llama-completion`. For each window it runs one
single-turn chat (`-cnv -st`) with the versioned system prompt
`context-reasoning-v1` (`internal/context/llamacpp/prompt.go`), the window's
evidence as compact JSON and a per-window JSON schema (`--json-schema-file`)
that restricts citations to that window's observation IDs and frame
timestamps. Generation uses temperature 0, seed 0 and `--offline`; inherited
`LLAMA_ARG_*` variables are removed and no download flag is passed, so nothing
touches the network. The answer is parsed strictly (unknown fields, missing or
out-of-range confidences, or anything but one JSON object fail the run).

| Option | Default | Meaning |
|---|---|---|
| `model` | required | GGUF instruction-tuned text model |
| `binary` | `llama-completion` on `PATH` | llama.cpp non-interactive CLI |
| `threads` | llama.cpp default | `-t` |
| `gpu-layers` | llama.cpp default | `-ngl` |
| `max-tokens` | `1024` | answer token limit (`-n`) |
| `ctx-size` | model default | context size (`-c`) |

On macOS with Homebrew (whisper.cpp and the vision model as in the sections
above):

```bash
brew install llama.cpp whisper-cpp ffmpeg  # llama-completion, llama-mtmd-cli, whisper-cli
# Download a text model manually, e.g. from Hugging Face
# Qwen/Qwen2.5-7B-Instruct-GGUF (qwen2.5-7b-instruct-q4_k_m*.gguf)
# or Qwen/Qwen2.5-3B-Instruct-GGUF (qwen2.5-3b-instruct-q4_k_m.gguf) on 8 GB Macs.
sed -e '/^audio:/,/^vision:/ s/provider: TBD/provider: whisper-cpp/' \
    -e '/^vision:/,/^fusion:/ s/provider: TBD/provider: llama-mtmd/' \
    -e 's/sampling: TBD/sampling: uniform:2/' \
    -e '/^fusion:/,/^schema:/ s/provider: TBD/provider: llama-cpp/' \
  configs/experiments/multimodal-5s.yaml > /tmp/e04.yaml
go run ./cmd/harness \
  --config /tmp/e04.yaml \
  --manifest manifests/<dataset-id>-v<dataset-version>.json \
  --dataset-root dataset \
  --output /tmp/results \
  --pipeline multimodal \
  --audio-option model="$HOME/models/ggml-large-v3-turbo.bin" --audio-option language=pt \
  --vision-option model="$HOME/models/Qwen2.5-VL-7B-Instruct-Q4_K_M.gguf" \
  --vision-option mmproj="$HOME/models/mmproj-Qwen2.5-VL-7B-Instruct-f16.gguf" \
  --context-option model="$HOME/models/qwen2.5-7b-instruct-q4_k_m.gguf"
```

The three models are loaded one after another per window, not at once; the
largest (the 7B VLM plus projector, about 8 GB) sets the memory floor. Every
call reloads its model, so expect tens of seconds per window on CPU; latency
is measured in M08. Offline tests use fakes; an opt-in integration test runs
the real reasoner on a synthetic window:

```bash
CONTEXT_VIDEO_LLM_MODEL=$HOME/models/qwen2.5-3b-instruct-q4_k_m.gguf \
go test -tags integration -run Integration -v ./internal/context/llamacpp
```

`CONTEXT_VIDEO_LLM_BINARY` overrides the `llama-completion` binary.

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
