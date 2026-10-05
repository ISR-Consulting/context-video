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

The committed E01 and E03–E05 configurations select `audio.provider:
whisper-cpp`, the only registered adapter.

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
go run ./cmd/harness \
  --config configs/experiments/audio-only-5s.yaml \
  --manifest manifests/<dataset-id>-v<dataset-version>.json \
  --dataset-root dataset \
  --output /tmp/results \
  --pipeline audio \
  --audio-option model="$HOME/models/ggml-large-v3-turbo.bin" \
  --audio-option language=pt
```

The dataset must reference approved `LOCAL` media. Offline tests use fakes; an
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
Scene-change selection is not implemented. The committed E02–E05
configurations select `vision.provider: llama-mtmd` with sampling chosen for a
roughly constant frame density: E02 and E04 `uniform:2`, E03 `uniform:1`, E05
`uniform:4`.

### llama.cpp multimodal (`llama-mtmd`)

The first adapter runs a local GGUF vision-language model (for example
Qwen2.5-VL) through `llama-mtmd-cli`. For each frame, ffmpeg (file protocol
only) extracts one JPEG and `llama-mtmd-cli` answers the versioned prompt
`vision-frame-v2` (`internal/vision/llamamtmd/prompt.go`) at temperature 0 and
a fixed seed. A GBNF grammar (`--grammar`) admits only compact JSON (no
indentation, so no tokens spent on whitespace), the contract types, at most 12
detections, and lowercase snake_case values for `OBJECT`, `TOPIC`, `SCENE` and
`ACTION`. The prompt asks for brands as `BRAND` rather than `TEXT` and for no
running clocks; later frames of a window are told which `ENTITY`, `BRAND` and
`TEXT` values an earlier frame already reported, so scoreboards and boards are
not repeated unless they change.
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
| `max-tokens` | `1024` | answer token limit (`-n`) |
| `max-edge` | `768` | longest frame edge in pixels |
| `ctx-size` | llama.cpp default | context size (`-c`); M08 pins `4096` |

On macOS with Homebrew:

```bash
brew install llama.cpp ffmpeg   # provides llama-mtmd-cli
# Download a model and its projector manually, e.g. from Hugging Face
# ggml-org/Qwen2.5-VL-7B-Instruct-GGUF:
#   Qwen2.5-VL-7B-Instruct-Q4_K_M.gguf and mmproj-Qwen2.5-VL-7B-Instruct-f16.gguf
# (ggml-org/Qwen2.5-VL-3B-Instruct-GGUF is the smaller alternative).
go run ./cmd/harness \
  --config configs/experiments/vision-only-5s.yaml \
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
- **Output.** The persisted `ExperimentResult` is the metrics envelope; every
  observation and ContextEvent is written next to it as raw JSON Lines (see
  [M08 experiment execution](#m08-experiment-execution)). Correlation, ordering, IDs and mapping are deterministic; model output is
  reproducible only for a fixed llama.cpp build, model, thread count and
  backend.

The reasoner is selected by the existing `fusion.provider` and configured with
repeatable `--context-option key=value` flags. `--audio-option` and
`--vision-option` apply to the multimodal pipeline as well. All committed
experiment configurations (E01–E05) select `fusion.provider: llama-cpp`, used
with a 7B text model: **Qwen2.5-7B-Instruct Q4_K_M** (for example
`Qwen2.5-7B-Instruct-Q4_K_M.gguf` from Hugging Face
`bartowski/Qwen2.5-7B-Instruct-GGUF`, about 4.7 GB). The model file is a
runtime `--context-option model=…`, not part of the configuration; only its
file name reaches provenance. Their audio and vision providers and sampling
are set too (see above), so the committed files run as they are.

### llama.cpp text model (`llama-cpp`)

The first reasoner runs a local GGUF instruction-tuned text model through
llama.cpp's non-interactive `llama-completion`. For each window it runs one
single-turn chat (`-cnv -st`) with the versioned system prompt
`context-reasoning-v2` (`internal/context/llamacpp/prompt.go`), the window's
evidence as compact JSON sources and a per-window JSON schema
(`--json-schema-file`). Sources are keyed `a1`, `a2`, … (transcripts) and `v1`,
`v2`, … (frames, with description and detections) and carry no observation IDs
and no perception confidences, so the model cannot copy a perception score.
The schema restricts entity types to the annotation guidelines' closed list
(`PERSON`, `SPORTS_TEAM`, `ORGANIZATION`, `PLACE`, `EVENT`), topic, object and
brand labels to English-style lowercase snake_case (proper names in entities
stay as written; countries and national teams in English), and requires every
item to list the source keys it comes from. A ContextEvent's evidence is the
union of its items' sources, so a label read from a frame always cites that
frame. Generation uses temperature 0, seed 0 and `--offline`; inherited
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
| `ctx-size` | model default | context size (`-c`); M08 pins `8192` |

On macOS with Homebrew (whisper.cpp and the vision model as in the sections
above):

First check that the Homebrew llama.cpp build ships `llama-completion`:

```bash
ls $(brew --prefix llama.cpp)/bin | grep llama-completion
```

If llama.cpp is not installed, install it with `brew install llama.cpp`. If it
is installed but has no `llama-completion`, pass the path of a llama.cpp build
that does with `--context-option binary=<path>`.

```bash
brew install llama.cpp whisper-cpp ffmpeg  # llama-completion, llama-mtmd-cli, whisper-cli
# Download the text model manually, e.g. from Hugging Face
# bartowski/Qwen2.5-7B-Instruct-GGUF: Qwen2.5-7B-Instruct-Q4_K_M.gguf
# (Qwen/Qwen2.5-3B-Instruct-GGUF qwen2.5-3b-instruct-q4_k_m.gguf is the
# smaller, noticeably weaker alternative for 8 GB Macs).
go run ./cmd/harness \
  --config configs/experiments/multimodal-5s.yaml \
  --manifest manifests/<dataset-id>-v<dataset-version>.json \
  --dataset-root dataset \
  --output /tmp/results \
  --pipeline multimodal \
  --audio-option model="$HOME/models/ggml-large-v3-turbo.bin" --audio-option language=pt \
  --vision-option model="$HOME/models/Qwen2.5-VL-7B-Instruct-Q4_K_M.gguf" \
  --vision-option mmproj="$HOME/models/mmproj-Qwen2.5-VL-7B-Instruct-f16.gguf" \
  --context-option model="$HOME/models/Qwen2.5-7B-Instruct-Q4_K_M.gguf" \
  --context-option ctx-size=8192
```

The three models are loaded one after another per window, not at once; the
largest (the 7B VLM plus projector, about 8 GB) sets the memory floor. Every
call reloads its model, so expect tens of seconds per window on CPU; latency
is measured in M08. Offline tests use fakes; an opt-in integration test runs
the real reasoner on a synthetic window:

```bash
CONTEXT_VIDEO_LLM_MODEL=$HOME/models/Qwen2.5-7B-Instruct-Q4_K_M.gguf \
go test -tags integration -run Integration -v ./internal/context/llamacpp
```

`CONTEXT_VIDEO_LLM_BINARY` overrides the `llama-completion` binary.

## M08 experiment execution

M08 runs E01–E05 against the Golden Dataset through `--pipeline multimodal`
(E01 and E02 included, so all five produce ContextEvents). The plan and its
approved decisions are recorded in the Project's M08 plan; the runnable parts
are:

- `scripts/m08-run.sh`: the runbook. It runs E01, E02, E04, E05 and E03 with
  Live pacing, then reruns E04 with instant (VOD) pacing and diffs the two raw
  outputs (the H4 check). `--pacing instant` runs the experiments with instant
  pacing instead, under `vod/`, without the separate rerun. The vision answer
  limit is `VLM_MAX_TOKENS` (default 1024; 512 truncates frames with many
  detections). Everything goes to
  `~/context-video-runs/m08/<run-id>/` (or `--runs-dir`, default
  `$CONTEXT_VIDEO_RUNS_DIR`), and a directory inside the repository is refused.
- `scripts/m08-record-clip.sh`: records one fixed 60–120 s clip from a YouTube
  live stream or past broadcast and prints its manifest entry. A live stream is
  not reproducible; the recorded clip is, and `--pacing live` replays it in
  media time. Rights and YouTube Terms of Service are the operator's call.
- `scripts/m08-make-smoke-dataset.sh`: generates a synthetic 20 s clip
  (drawn scoreboard, synthetic pt-BR speech) with manifest and ground truth,
  for smoke runs on any host. It is not Golden Dataset evidence.
- `dataset/ANNOTATION-GUIDELINES.md` (v1.0) and `dataset/templates/`: the
  annotation rules and the manifest and ground-truth skeletons for the clips.

### Harness flags (multimodal only)

| Flag | Default | Meaning |
|---|---|---|
| `--pacing` | `instant` | `instant`: segments back to back (VOD-like). `live`: each test case is replayed from media time 0 by the M04 simulator and a segment starts only once its window has played |
| `--speed` | `1` | live replay speed (1 = real time) |
| `--on-segment-error` | `fail` | `fail` aborts the run; `record` traces the failed segment (no ContextEvents from it, its observations kept) and continues. Cancellation always aborts |
| `--cost-per-hour` | unset | host price in USD per wall-clock hour; enables `costPerVideoHour` |

Processing stays sequential. On CPU and probably on a Mac every call reloads
its model and the pipeline is slower than real time, so Live latency is
dominated by queueing; that is a finding, not a bug.

### Artifacts

A multimodal run writes `<output>/<experiment-id>/` and refuses to start if
it exists:

```text
<dataset-id>-v<ver>.json          ExperimentResult (metrics filled, schema unchanged)
raw/<test-case-id>/audio-observations.jsonl
raw/<test-case-id>/visual-observations.jsonl
raw/<test-case-id>/context-events.jsonl   one schema-valid contract object per line
trace/<test-case-id>.jsonl        one record per segment, appended as it finishes: due, start and
                                  finish times, per-call stage timing, event IDs, failure stage/error
summary.json                      stage percentiles, queue wait, real-time factor, failures by stage
run-manifest.json                 pacing, policy, host, dataset, adapter options (files by base name
                                  and SHA-256, never host paths), status; written for failed runs too
```

Metrics (a metric without samples is absent):

- `latencyP50/P95/P99Ms`: Live only, one sample per ContextEvent: the time
  the segment's events were available minus the time the window's first
  instant played on the replay clock.
- `schemaCompliance`: validated contract objects ÷ emitted. Invalid output
  aborts a run, so a completed run scores 1 by construction.
- `evidenceTraceability`: events whose every evidence item resolves to an
  observation of the same test case ÷ events.
- `contextStability`: per test case, the mean Jaccard similarity of the
  label sets of adjacent windows (both-empty pairs skipped), averaged over test
  cases. Labels are compared lexically.
- `costPerVideoHour`: `--cost-per-hour` × processing time ÷ media time.

Confidences in the raw artifacts are uncalibrated model self-assessments, not
probabilities. Quality scoring against the ground truth is M09.

### Canonical run (MacBook Pro, 16 GB)

Canonical runs must use the v2 prompts: `vision-frame-v2` and
`context-reasoning-v2` (the latter is recorded in every ContextEvent's
`provenance.promptVersion`). The trial runs `trial-football-01` and
`trial-all-02` used the v1 prompts and are not comparable with canonical
results.

```bash
brew install llama.cpp whisper-cpp ffmpeg yt-dlp
ls $(brew --prefix llama.cpp)/bin | grep -E 'llama-(completion|mtmd-cli)'
# ~/models: ggml-large-v3-turbo.bin, Qwen2.5-VL-7B-Instruct-Q4_K_M.gguf,
#           mmproj-Qwen2.5-VL-7B-Instruct-f16.gguf, Qwen2.5-7B-Instruct-Q4_K_M.gguf
scripts/m08-record-clip.sh --url 'https://www.youtube.com/watch?v=<id>' \
  --id football-live-01 --scenario FOOTBALL_SPORTS_APPAREL      # repeat per clip
cp dataset/templates/poc-golden-v1.0.json dataset/manifests/     # paste the printed entries
# write dataset/ground-truth/poc-golden/<test-case-id>.json per the guidelines
go run ./cmd/harness --config configs/experiments/multimodal-5s.yaml \
  --manifest manifests/poc-golden-v1.0.json --output /tmp/check --pipeline validation-only
# On AC power; caffeinate keeps macOS from sleeping, which would distort latency.
caffeinate -dimsu scripts/m08-run.sh --run-id calibrate --experiments E04 --no-vod   # time one experiment first
caffeinate -dimsu scripts/m08-run.sh --run-id 2026-10-m5-7b
```

Peak memory is about 7 GB (the vision model plus projector); the three
models load one after another. The H4 check (`h4-check.txt`) compares E04 live and VOD
raw outputs byte for byte. In the VM smoke run the CPU build of
`llama-mtmd-cli` (b11295) answered differently for the same image at
`--temp 0 --seed 0`, even with one thread, so a non-empty diff first needs
checking whether only provider outputs (visual observations and what follows
from them) differ. Smoke run on any host:

```bash
scripts/m08-make-smoke-dataset.sh ~/context-video-smoke
WHISPER_MODEL=ggml-small.bin VLM_MODEL=Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf \
VLM_MMPROJ=mmproj-Qwen2.5-VL-3B-Instruct-Q8_0.gguf LLM_MODEL=qwen2.5-3b-instruct-q4_k_m.gguf \
scripts/m08-run.sh --run-id smoke --dataset-root ~/context-video-smoke \
  --manifest manifests/poc-smoke-v0.1.json
```

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
