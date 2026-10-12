# M09 — Evaluation report

Canonical run: **`2026-10-07-m5-7b-v2`**  
Git: **`899e7ef`** · Host: MacBook Pro M5, 16 GB · Models: whisper `large-v3-turbo` (pt), Qwen2.5-VL-7B Q4_K_M (`max-tokens` 1024, `ctx-size` 4096), Qwen2.5-7B-Instruct Q4_K_M reasoner (`context-reasoning-v2`, `ctx-size` 8192)  
Raw artifacts: operator-local (not in this repo). Metrics and quality tables below are from that zip only.

Governed by POC-SPEC §8–15, CONTRACTS-V1, ADR-001/002/003/005/007/008, M08 decisions D8–D13.

## 1. Experiment matrix

| ID | Audio | Vision | Window | Sampling | Pacing | Status |
|---|---|---|---:|---|---|---|
| E01 | whisper-cpp | — | 5 s | — | Live 1× | completed |
| E02 | — | llama-mtmd | 5 s | uniform:2 | Live 1× | completed |
| E03 | whisper-cpp | llama-mtmd | 2 s | uniform:1 | Live 1× | completed |
| E04 | whisper-cpp | llama-mtmd | 5 s | uniform:2 | Live 1× + VOD instant | completed |
| E05 | whisper-cpp | llama-mtmd | 10 s | uniform:4 | Live 1× | completed |

Dataset: four 120 s clips (`football-live-01`, `visual-cooking-01`, `auditory-podcast-01`, `ambiguous-podpah-01`); 480 s media total per experiment.

## 2. Latency and throughput (Live)

| Exp | Segments | Failed | RTF | Segment p50 (ms) | CAL p50 (ms) | CAL p95 (ms) | Queue wait p50 (ms) |
|---|---:|---:|---:|---:|---:|---:|---:|
| E01 | 96 | 4 | **0.98** | 5 265 | 15 165 | 24 664 | 3 698 |
| E02 | 96 | 2 | 4.61 | 22 450 | 233 314 | 435 690 | 210 299 |
| E03 | 240 | 5 | 7.89 | 15 161 | 417 698 | 804 677 | 402 518 |
| E04 | 96 | 5 | 4.80 | 23 622 | 233 330 | 460 650 | 215 956 |
| E05 | 48 | 4 | 3.95 | 38 992 | 200 200 | 373 933 | 151 171 |

CAL = Context Availability Latency (FinishedAt − window start on the replay clock), one sample per emitted ContextEvent (M08 D8).

Stage totals (Live):

| Exp | Audio total (s) | Vision total (s) | Reason total (s) | Vision per-frame p50 (ms) |
|---|---:|---:|---:|---:|
| E01 | 101 | — | 371 | — |
| E02 | — | 1 460 | 755 | 7 382 |
| E03 | 259 | 1 884 | 1 643 | 7 256 |
| E04 | 110 | 1 443 | 752 | 7 297 |
| E05 | 58 | 1 376 | 461 | 7 103 |

**H5 finding:** audio-only is near real-time (RTF ≈ 1). Any vision path is **not** real-time on this M5 7B stack (RTF ≈ 4–8). Live latency is dominated by the processing backlog, not by waiting for window fill.

## 3. Stability, schema, traceability

From `ExperimentResult.metrics` (Live):

| Exp | schemaCompliance | evidenceTraceability | contextStability (adjacent Jaccard) |
|---|---:|---:|---:|
| E01 | 1.0 | 1.0 | 0.060 |
| E02 | 1.0 | 1.0 | 0.231 |
| E03 | 1.0 | 1.0 | 0.158 |
| E04 | 1.0 | 1.0 | 0.162 |
| E05 | 1.0 | 1.0 | 0.134 |

Notes (ADR-008 / M08 C4):

- Schema and evidence ratios are **1.0 by construction** for a completed harness run (invalid objects abort the segment; emitted objects always validated).
- Stability is **lexical** Jaccard of adjacent windows (no synonym fold). Low values partly reflect label churn (`podpah` vs `podpah_tv`, brand spelling variants), not only semantic oscillation.
- **Cost:** `costPerVideoHour` is **absent** — the run did not pass `--cost-per-hour`. RTF is the cost proxy recorded in summaries.

## 4. Segment failures (stability of the pipeline)

Vision JSON truncation from earlier trials is **fixed** (0 vision-stage failures in this run). Remaining failures are all **reason** stage, typically duplicate labels in one candidate (example: `duplicate entity EVENT "corner_kick"`).

| Exp | Failed segments | Failed stage |
|---|---:|---|
| E01 | 4 / 96 | reason |
| E02 | 2 / 96 | reason |
| E03 | 5 / 240 | reason |
| E04 | 5 / 96 | reason |
| E05 | 4 / 48 | reason |

## 5. H4 — Shared Live / VOD core

E04 Live vs E04 VOD (`--pacing instant`): **raw ContextEvent outputs are identical** (`h4-check.txt`: “E04 live and VOD raw outputs are identical”). VOD summary RTF 4.60 vs Live 4.80 (Live includes pacing/queue accounting; processing content matches).

Hypothesis **H4 supported** for this stack: same observation, fusion and ContextEvent contracts; pacing is the main execution difference.

## 6. Quality vs ground truth

### 6.1 Scorer status

A **minimal offline lexical scorer** is implemented in `internal/evaluation/quality`:

- Match windows by media-time **overlap**.
- Exact string equality on labels (entity `type`+`value`; topic/object/brand `value`).
- `required` → recall; `optional` ignored; `forbidden` → forbidden-hit count.
- **No** synonym / stem / case-fold / language normalization.

**Deferred:** synonym / near-match policy (needed for fair cooking / podcast / podpah scores). Quality numbers are **not** written into `ExperimentResult` (would be a schema bump). Tables below were computed offline from the stored run’s `context-events.jsonl` (not committed) against the committed ground truth.

### 6.2 Clip-level required recall (lexical)

| Exp | football | cooking | podcast | podpah |
|---|---:|---:|---:|---:|
| E01 (audio) | 0.12 | 0.00 | 0.06 | 0.00 |
| E02 (vision) | 0.56 | 0.44 | 0.22 | 0.38 |
| E03 (A+V 2 s) | **0.75** | 0.44 | **0.50** | **0.46** |
| E04 (A+V 5 s) | 0.50 | 0.44 | 0.22 | 0.23 |
| E05 (A+V 10 s) | 0.38 | 0.33 | 0.22 | 0.23 |

### 6.3 Qualitative notes (E04 Live)

- **Football / brands:** strongest exact matches (`Germany`, `Serbia`, `football`, `adidas`, `bitvavo`, `ergo`, `football_jersey`). Misses often synonym/role issues (`goal` as topic vs object, `corner_kick`, `Allianz Arena`, OCR/ASR name variants).
- **Cooking:** `cooking`, `carrot`, `apron`, `blender` hit; oil / egg / jar labels frequently miss or land in wrong families (`ASMR` as brand/org).
- **Podcast / Podpah:** lexical under-count — `pod_delas` / `poddelas`, `podpah` / `podpah_tv`, `FYS` as org vs brand `fys`. Topics like `faith`, `health`, `challenge` rarely exact-match.

**H1:** useful structured context is produced (esp. football).  
**H2 (provisional, lexical):** vision lifts quality over audio-only; multimodal E03/E04 beat E01, but E02 is competitive with E04 on exact match — fair H2 needs synonym-aware scoring.  
**H3 (provisional):** denser 2 s windows (E03) show the highest lexical recall here; 10 s (E05) is weaker on exact required hits. Confounded by failure rate and label churn.

## 7. Architecture findings

1. **Contracts held:** Live and VOD share observation / fusion / ContextEvent paths (ADR-005); evidence and provenance present (`promptVersion: context-reasoning-v2`).
2. **Sequential A→V→reason** on one Metal host makes vision the bottleneck (~⅔ of multimodal time); model reload per call adds fixed cost but generation dominates (prior trial).
3. **Fail-soft `--on-segment-error=record`** preserved the run through reasoner duplicate-label rejects.
4. **Context Core stayed commerce-free** (ADR-001); confidences remain uncalibrated (ADR-008).
5. **Golden annotations** are now in-repo; media remains operator-local. Committed manifest uses `CONTROLLED_SOURCE` so loaders work without Globo bytes — convert to `LOCAL` + Mac digests for harness re-runs.

## 8. Recommendations

1. **Persistent servers** (whisper-server / llama-server): modest win (~1.15–1.4×) by cutting reload; not enough alone for Live real-time with 7B vision.
2. **Parallel audio + vision** per window: cuts wall time toward `max(A,V)+reason` instead of `A+V+reason`; still vision-bound.
3. **Cloud / hosted VLMs** (Claude, OpenAI, etc.): only after content-rights approval (POC-SPEC open Q9); would change latency/cost profile and need new adapters behind existing ports.
4. **Reasoner hygiene:** dedupe labels before domain validation (or soften duplicate rejection) to cut the remaining ~2–8% segment loss.
5. **Synonym policy for M09.1:** small closed map (`podpah`↔`podpah_tv`, country aliases already English in GT) before claiming H2/H3 numerically.

## 9. Open items

| Item | Status |
|---|---|
| `costPerVideoHour` | Absent — supply `--cost-per-hour` on a future run or leave RTF-only. |
| Synonym / near-match policy | Deferred; lexical scorer shipped. |
| LOCAL media digests in manifest | Operator Mac digests not in store; committed as `CONTROLLED_SOURCE`. Paste LOCAL sha256s when available. |
| Product quality / latency SLAs | Still TBD in POC-SPEC §13 — not treated as SLAs. |
| Cloud provider experiment | Optional; blocked on API keys + rights. |

## 10. M09 slice delivered in this change

- M08 marked complete in `MILESTONES.md`.
- This report.
- `dataset/manifests/poc-golden-v1.0.json` + four ground-truth files.
- `internal/evaluation/quality` lexical overlap scorer + tests.

Not delivered: synonym scorer, cost figures, committed raw run dump, schema changes.
