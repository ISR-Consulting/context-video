#!/usr/bin/env bash
# Records one fixed Golden Dataset clip from a YouTube live stream (or a
# section of a past broadcast), normalized to H.264/AAC MP4 with both a video
# and an audio stream, and prints the manifest testCases entry for it.
#
# A live stream itself is not reproducible: record fixed clips, annotate them
# and replay them with `harness --pacing live` (the M04 simulator). Whether you
# may record and use a given stream (rights, YouTube Terms of Service) is your
# decision; this script does not check it.
#
# usage:
#   scripts/m08-record-clip.sh --url URL --id TEST_CASE_ID --scenario SCENARIO [options]
#
#   --url URL            YouTube watch URL
#   --id ID              test-case id, lowercase kebab-case (e.g. football-live-01)
#   --scenario S         FOOTBALL_SPORTS_APPAREL | PREDOMINANTLY_VISUAL |
#                        PREDOMINANTLY_AUDITORY | MULTIMODAL_AMBIGUOUS
#   --section START      past broadcast: record from START (HH:MM:SS) instead of "now"
#   --duration SECONDS   clip length, 60..120, default 120
#   --dataset-root DIR   default dataset (media goes to DIR/media/ID.mp4, git-ignored)
#   --content-id ID      default yt-<video-id>-<UTC timestamp>
#
# Requires yt-dlp, ffmpeg and ffprobe (macOS: brew install yt-dlp ffmpeg).
set -euo pipefail

die() { echo "m08-record-clip: $*" >&2; exit 2; }

url="" id="" scenario="" section="" duration=120 dataset_root="dataset" content_id=""
while [ $# -gt 0 ]; do
  case "$1" in
    --url) url="${2:?}"; shift 2 ;;
    --id) id="${2:?}"; shift 2 ;;
    --scenario) scenario="${2:?}"; shift 2 ;;
    --section) section="${2:?}"; shift 2 ;;
    --duration) duration="${2:?}"; shift 2 ;;
    --dataset-root) dataset_root="${2:?}"; shift 2 ;;
    --content-id) content_id="${2:?}"; shift 2 ;;
    -h|--help) sed -n '2,24p' "$0"; exit 0 ;;
    *) die "unknown argument $1 (see --help)" ;;
  esac
done
[ -n "$url" ] && [ -n "$id" ] && [ -n "$scenario" ] || die "--url, --id and --scenario are required"
case "$id" in *[!a-z0-9-]*|-*|"") die "--id must be lowercase kebab-case" ;; esac
case "$scenario" in
  FOOTBALL_SPORTS_APPAREL|PREDOMINANTLY_VISUAL|PREDOMINANTLY_AUDITORY|MULTIMODAL_AMBIGUOUS) ;;
  *) die "unknown --scenario $scenario" ;;
esac
case "$duration" in *[!0-9]*|"") die "--duration must be whole seconds" ;; esac
[ "$duration" -ge 60 ] && [ "$duration" -le 120 ] || die "--duration must be 60..120 seconds"
for tool in yt-dlp ffmpeg ffprobe; do command -v "$tool" >/dev/null 2>&1 || die "$tool not found"; done

out="$dataset_root/media/$id.mp4"
[ ! -e "$out" ] || die "$out already exists; an annotated clip must never be re-recorded or re-encoded"
mkdir -p "$dataset_root/media"
recorded_at="$(date -u +%Y%m%dT%H%M%SZ)"
video_id="$(yt-dlp --get-id "$url" | head -1)"
[ -n "$content_id" ] || content_id="yt-$(echo "$video_id" | tr 'A-Z_' 'a-z-')-$(echo "$recorded_at" | tr 'A-Z' 'a-z')"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

normalize() {
  ffmpeg -hide_banner -loglevel error -y -i "$1" -t "$duration" -map 0:v:0 -map 0:a:0 \
    -c:v libx264 -preset veryfast -crf 20 -r 25 -pix_fmt yuv420p \
    -c:a aac -ar 48000 -ac 2 -movflags +faststart "$out"
}
if [ -z "$section" ]; then
  content_type="LIVE"
  echo "recording ${duration}s of $url from now..." >&2
  # ffmpeg stops after -t; yt-dlp then exits on the closed pipe.
  yt-dlp -q -f 'b[height<=720][vcodec!=none][acodec!=none]' -o - "$url" | normalize pipe:0 || [ -s "$out" ]
else
  content_type="VOD"
  end="$(awk -v s="$section" -v d="$duration" 'BEGIN{n=split(s,p,":"); t=0; for(i=1;i<=n;i++) t=t*60+p[i]; t+=d; printf "%02d:%02d:%02d", t/3600, (t%3600)/60, t%60}')"
  echo "downloading section $section-$end of $url..." >&2
  yt-dlp -q --download-sections "*$section-$end" --force-keyframes-at-cuts \
    -f 'bv*[height<=720]+ba/b[height<=720]' --merge-output-format mp4 -o "$tmp/raw.mp4" "$url"
  normalize "$tmp/raw.mp4"
fi

streams="$(ffprobe -v error -show_entries stream=codec_type -of csv=p=0 "$out" | sort -u | tr '\n' ' ')"
[ "$streams" = "audio video " ] || { rm -f "$out"; die "clip needs one audio and one video stream, got: $streams"; }
# durationMs is the shortest stream, floored, so no window reaches past either stream.
duration_ms="$(ffprobe -v error -show_entries stream=duration -of csv=p=0 "$out" |
  awk 'NR==1||$1<m{m=$1} END{printf "%d", m*1000}')"
if command -v sha256sum >/dev/null 2>&1; then digest="$(sha256sum "$out" | cut -d' ' -f1)"; else digest="$(shasum -a 256 "$out" | cut -d' ' -f1)"; fi

echo "recorded $out (${duration_ms} ms). Add this entry to the manifest testCases:" >&2
cat <<EOF
{
  "testCaseId": "$id",
  "scenario": "$scenario",
  "content": {"contentId": "$content_id", "contentType": "$content_type"},
  "media": {"kind": "LOCAL", "uri": "media/$id.mp4", "sha256": "$digest", "durationMs": $duration_ms},
  "groundTruthPath": "ground-truth/poc-golden/$id.json",
  "description": "Recorded ${recorded_at} from $url${section:+ at $section}.",
  "tags": ["youtube", "$(echo "$content_type" | tr 'A-Z' 'a-z')"]
}
EOF
