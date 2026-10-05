#!/usr/bin/env bash
# M08 runbook: runs E01–E05 with Live pacing (or instant pacing with
# --pacing instant) plus an E04 VOD (instant) rerun through the harness
# multimodal pipeline, with one set of models, and writes
# every artifact outside the repository.
#
# usage: scripts/m08-run.sh --run-id ID [options]
#
#   --run-id ID          required; output goes to RUNS_DIR/m08/ID
#   --runs-dir DIR       default $CONTEXT_VIDEO_RUNS_DIR or ~/context-video-runs
#   --dataset-root DIR   default dataset
#   --manifest PATH      relative to --dataset-root; default manifests/poc-golden-v1.0.json
#   --models-dir DIR     default ~/models
#   --experiments LIST   comma-separated subset, default E01,E02,E04,E05,E03
#   --pacing P           live (default; output under live/) or instant (output under
#                        vod/; the separate E04 VOD rerun and H4 check are then skipped)
#   --speed X            live pacing speed, default 1 (real time)
#   --on-segment-error P record (default) or fail
#   --cost-per-hour USD  optional host price for costPerVideoHour
#   --threads N          optional -t for every model
#   --no-vod             skip the E04 VOD rerun
#   --dry-run            print the commands only
#
# Model files (relative to --models-dir unless absolute) and binaries can be
# overridden with environment variables:
#   WHISPER_MODEL (ggml-large-v3-turbo.bin)  WHISPER_LANGUAGE (pt)
#   VLM_MODEL (Qwen2.5-VL-7B-Instruct-Q4_K_M.gguf)
#   VLM_MMPROJ (mmproj-Qwen2.5-VL-7B-Instruct-f16.gguf)  VLM_CTX_SIZE (4096)
#   VLM_MAX_TOKENS (1024): vision answer limit; 512 truncates frames with many detections
#   LLM_MODEL (Qwen2.5-7B-Instruct-Q4_K_M.gguf)  LLM_CTX_SIZE (8192)
#   WHISPER_CLI, LLAMA_MTMD_CLI, LLAMA_COMPLETION, FFMPEG: binaries (default: on PATH)
#
# This script is bash 3.2 compatible (macOS /bin/bash).
set -euo pipefail

die() { echo "m08-run: $*" >&2; exit 2; }

run_id=""
runs_dir="${CONTEXT_VIDEO_RUNS_DIR:-$HOME/context-video-runs}"
dataset_root="dataset"
manifest="manifests/poc-golden-v1.0.json"
models_dir="$HOME/models"
experiments="E01,E02,E04,E05,E03"
pacing="live"
speed="1"
speed_set=0
segment_errors="record"
cost=""
threads=""
vod=1
dry=0
while [ $# -gt 0 ]; do
  case "$1" in
    --run-id) run_id="${2:?}"; shift 2 ;;
    --runs-dir) runs_dir="${2:?}"; shift 2 ;;
    --dataset-root) dataset_root="${2:?}"; shift 2 ;;
    --manifest) manifest="${2:?}"; shift 2 ;;
    --models-dir) models_dir="${2:?}"; shift 2 ;;
    --experiments) experiments="${2:?}"; shift 2 ;;
    --pacing) pacing="${2:?}"; shift 2 ;;
    --speed) speed="${2:?}"; speed_set=1; shift 2 ;;
    --on-segment-error) segment_errors="${2:?}"; shift 2 ;;
    --cost-per-hour) cost="${2:?}"; shift 2 ;;
    --threads) threads="${2:?}"; shift 2 ;;
    --no-vod) vod=0; shift ;;
    --dry-run) dry=1; shift ;;
    -h|--help) awk 'NR > 1 && /^#/ { sub(/^# ?/, ""); print; next } NR > 1 { exit }' "$0"; exit 0 ;;
    *) die "unknown argument $1 (see --help)" ;;
  esac
done
[ -n "$run_id" ] || die "--run-id is required"
case "$pacing" in
  live) pacing_dir="live" ;;
  instant)
    pacing_dir="vod"
    [ "$speed_set" -eq 0 ] || die "--speed is only valid with --pacing live"
    vod=0
    ;;
  *) die "unknown --pacing $pacing; available: live, instant" ;;
esac
case "$run_id" in *[!A-Za-z0-9._-]*|.*) die "--run-id must match [A-Za-z0-9][A-Za-z0-9._-]*" ;; esac

repo="$(cd "$(dirname "$0")/.." && pwd -P)"
cd "$repo"
mkdir -p "$runs_dir"
runs_abs="$(cd "$runs_dir" && pwd -P)"
case "$runs_abs/" in "$repo"/*) die "--runs-dir $runs_abs is inside the repository; raw outputs must stay outside it" ;; esac
run_dir="$runs_abs/m08/$run_id"
[ ! -e "$run_dir" ] || die "$run_dir already exists; choose another --run-id"

model() { case "$1" in /*) echo "$1" ;; *) echo "$models_dir/$1" ;; esac; }
whisper_model="$(model "${WHISPER_MODEL:-ggml-large-v3-turbo.bin}")"
vlm_model="$(model "${VLM_MODEL:-Qwen2.5-VL-7B-Instruct-Q4_K_M.gguf}")"
vlm_mmproj="$(model "${VLM_MMPROJ:-mmproj-Qwen2.5-VL-7B-Instruct-f16.gguf}")"
llm_model="$(model "${LLM_MODEL:-Qwen2.5-7B-Instruct-Q4_K_M.gguf}")"
if [ "$dry" -eq 0 ]; then
  for f in "$whisper_model" "$vlm_model" "$vlm_mmproj" "$llm_model"; do
    [ -f "$f" ] || die "model file not found: $f"
  done
  [ -f "$dataset_root/$manifest" ] || die "manifest not found: $dataset_root/$manifest"
fi

config_for() {
  case "$1" in
    E01) echo configs/experiments/audio-only-5s.yaml ;;
    E02) echo configs/experiments/vision-only-5s.yaml ;;
    E03) echo configs/experiments/multimodal-2s.yaml ;;
    E04) echo configs/experiments/multimodal-5s.yaml ;;
    E05) echo configs/experiments/multimodal-10s.yaml ;;
    *) die "unknown experiment $1" ;;
  esac
}

# harness_args EXP PACING OUTPUT: prints one argument per line.
harness_args() {
  local exp="$1" pacing="$2" output="$3"
  printf '%s\n' --config "$(config_for "$exp")" --manifest "$manifest" --dataset-root "$dataset_root" \
    --specs-root specs --output "$output" --pipeline multimodal --pacing "$pacing" --on-segment-error "$segment_errors"
  [ "$pacing" = live ] && printf '%s\n' --speed "$speed"
  [ -n "$cost" ] && printf '%s\n' --cost-per-hour "$cost"
  if [ "$exp" != E02 ]; then
    printf '%s\n' --audio-option "model=$whisper_model" --audio-option "language=${WHISPER_LANGUAGE:-pt}"
    [ -n "${WHISPER_CLI:-}" ] && printf '%s\n' --audio-option "binary=$WHISPER_CLI"
    [ -n "${FFMPEG:-}" ] && printf '%s\n' --audio-option "ffmpeg=$FFMPEG"
    [ -n "$threads" ] && printf '%s\n' --audio-option "threads=$threads"
  fi
  if [ "$exp" != E01 ]; then
    printf '%s\n' --vision-option "model=$vlm_model" --vision-option "mmproj=$vlm_mmproj" \
      --vision-option "ctx-size=${VLM_CTX_SIZE:-4096}" --vision-option "max-tokens=${VLM_MAX_TOKENS:-1024}"
    [ -n "${LLAMA_MTMD_CLI:-}" ] && printf '%s\n' --vision-option "binary=$LLAMA_MTMD_CLI"
    [ -n "${FFMPEG:-}" ] && printf '%s\n' --vision-option "ffmpeg=$FFMPEG"
    [ -n "$threads" ] && printf '%s\n' --vision-option "threads=$threads"
  fi
  printf '%s\n' --context-option "model=$llm_model" --context-option "ctx-size=${LLM_CTX_SIZE:-8192}"
  [ -n "${LLAMA_COMPLETION:-}" ] && printf '%s\n' --context-option "binary=$LLAMA_COMPLETION"
  [ -n "$threads" ] && printf '%s\n' --context-option "threads=$threads"
  return 0
}

run_one() {
  local exp="$1" pacing="$2" output="$3" args=() line
  while IFS= read -r line; do args+=("$line"); done < <(harness_args "$exp" "$pacing" "$output")
  if [ "$dry" -eq 1 ]; then
    printf 'harness'; printf ' %q' "${args[@]}"; printf '\n'
    return 0
  fi
  echo "== $exp ($pacing) -> $output/$exp"
  "$run_dir/harness" "${args[@]}" 2>&1 | tee -a "$run_dir/$exp-$pacing.log"
}

if [ "$dry" -eq 0 ]; then
  mkdir -p "$run_dir"
  go build -o "$run_dir/harness" ./cmd/harness
  {
    echo "run-id: $run_id"
    echo "started: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "git: $(git rev-parse HEAD 2>/dev/null || echo unknown)$(git diff --quiet 2>/dev/null || echo ' (dirty)')"
    echo "os: $(uname -srm)"
    if command -v sw_vers >/dev/null 2>&1; then echo "macos: $(sw_vers -productVersion)"; fi
    if command -v sysctl >/dev/null 2>&1; then
      echo "cpu: $(sysctl -n machdep.cpu.brand_string 2>/dev/null || true)"
      echo "memory-bytes: $(sysctl -n hw.memsize 2>/dev/null || true)"
    fi
    echo "go: $(go version)"
    echo "ffmpeg: $("${FFMPEG:-ffmpeg}" -version 2>/dev/null | head -1)"
    echo "llama-completion: $("${LLAMA_COMPLETION:-llama-completion}" --version 2>&1 | grep -m1 -i version || true)"
    echo "llama-mtmd-cli: $("${LLAMA_MTMD_CLI:-llama-mtmd-cli}" --version 2>&1 | grep -m1 -i version || true)"
    echo "whisper-cli: $(command -v "${WHISPER_CLI:-whisper-cli}" || echo missing)"
    echo "models: $(basename "$whisper_model") $(basename "$vlm_model") $(basename "$vlm_mmproj") $(basename "$llm_model")"
    echo "experiments: $experiments; pacing: $pacing; speed: $speed; segment errors: $segment_errors; vod rerun: $vod"
    echo "vision: ctx-size ${VLM_CTX_SIZE:-4096}, max-tokens ${VLM_MAX_TOKENS:-1024}; reasoner: ctx-size ${LLM_CTX_SIZE:-8192}"
  } >"$run_dir/environment.txt"
fi

status=0
old_ifs="$IFS"; IFS=','
for exp in $experiments; do
  IFS="$old_ifs"
  run_one "$exp" "$pacing" "$run_dir/$pacing_dir" || { status=1; echo "m08-run: $exp $pacing failed; continuing" >&2; }
  IFS=','
done
IFS="$old_ifs"

if [ "$vod" -eq 1 ]; then
  run_one E04 instant "$run_dir/vod" || { status=1; echo "m08-run: E04 vod failed" >&2; }
  if [ "$dry" -eq 0 ] && [ -d "$run_dir/live/E04/raw" ] && [ -d "$run_dir/vod/E04/raw" ]; then
    if diff -r "$run_dir/live/E04/raw" "$run_dir/vod/E04/raw" >"$run_dir/h4-live-vs-vod.diff"; then
      echo "H4 check: E04 live and VOD raw outputs are identical" | tee "$run_dir/h4-check.txt"
    else
      echo "H4 check: E04 live and VOD raw outputs differ; see h4-live-vs-vod.diff" | tee "$run_dir/h4-check.txt"
    fi
  fi
fi
[ "$dry" -eq 1 ] || echo "m08-run: artifacts in $run_dir"
exit "$status"
