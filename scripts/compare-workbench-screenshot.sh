#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
    printf 'usage: bash scripts/compare-workbench-screenshot.sh REFERENCE.png CANDIDATE.png\n' >&2
    exit 2
fi

for tool in ffmpeg ffprobe cmp; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        printf '%s is required for workbench screenshot comparison\n' "$tool" >&2
        exit 2
    fi
done
for image in "$@"; do
    if [[ ! -f "$image" ]]; then
        printf 'missing screenshot: %s\n' "$image" >&2
        exit 1
    fi
    codec="$(ffprobe -v error -select_streams v:0 -show_entries stream=codec_name -of default=noprint_wrappers=1:nokey=1 "$image")"
    if [[ "$codec" != 'png' ]]; then
        printf 'screenshot must be lossless PNG: %s is %s\n' "$image" "$codec" >&2
        exit 1
    fi
    dimensions="$(ffprobe -v error -select_streams v:0 -show_entries stream=width,height -of csv=p=0:s=x "$image")"
    if [[ "$dimensions" != '740x520' ]]; then
        printf 'workbench screenshot must be 740x520: %s is %s\n' "$image" "$dimensions" >&2
        exit 1
    fi
done

work="$(mktemp -d /tmp/markstack-screenshot-compare.XXXXXX)"
trap 'rm -rf "$work"' EXIT
ffmpeg -hide_banner -loglevel error -i "$1" \
    -vf 'drawbox=x=15:y=495:w=482:h=24:color=black:t=fill' \
    -pix_fmt rgba -f rawvideo "$work/reference.rgba"
ffmpeg -hide_banner -loglevel error -i "$2" \
    -vf 'drawbox=x=15:y=495:w=482:h=24:color=black:t=fill' \
    -pix_fmt rgba -f rawvideo "$work/candidate.rgba"
if ! cmp -s "$work/reference.rgba" "$work/candidate.rgba"; then
    printf 'workbench pixels differ outside the workspace status path\n' >&2
    exit 1
fi
