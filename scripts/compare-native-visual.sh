#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
    printf 'usage: bash scripts/compare-native-visual.sh TYPORA.png MARKSTACK.png OUTPUT_DIR\n' >&2
    exit 2
fi

reference="$1"
candidate="$2"
output="$3"
for tool in ffmpeg ffprobe rg shasum; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        printf '%s is required for native visual comparison\n' "$tool" >&2
        exit 2
    fi
done
for image in "$reference" "$candidate"; do
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
    if [[ "$dimensions" != '3210x1910' ]]; then
        printf 'screenshot must be 3210x1910 (1605x955 at 2x): %s is %s\n' "$image" "$dimensions" >&2
        exit 1
    fi
done

mkdir -p "$output"
ffmpeg -hide_banner -nostats -i "$reference" -i "$candidate" \
    -filter_complex '[0:v]format=rgb24[a];[1:v]format=rgb24[b];[a][b]psnr' \
    -f null - 2> "$output/full-psnr.log"
ffmpeg -hide_banner -nostats -i "$reference" -i "$candidate" \
    -filter_complex '[0:v]crop=2000:1650:900:180,format=rgb24[a];[1:v]crop=2000:1650:900:180,format=rgb24[b];[a][b]psnr' \
    -f null - 2> "$output/article-psnr.log"
ffmpeg -hide_banner -nostats -i "$reference" -i "$candidate" \
    -filter_complex '[0:v]crop=2000:1650:900:180,format=gray[a];[1:v]crop=2000:1650:900:180,format=gray[b];[a][b]ssim' \
    -f null - 2> "$output/article-ssim.log"
ffmpeg -hide_banner -loglevel error -y -i "$reference" -i "$candidate" \
    -filter_complex '[0:v]format=rgb24[a];[1:v]format=rgb24[b];[a][b]blend=all_mode=difference' \
    -frames:v 1 -update 1 "$output/difference.png"

{
    printf 'full RGB '
    rg --no-filename -o 'PSNR r:[^ ]+ g:[^ ]+ b:[^ ]+ average:[^ ]+' "$output/full-psnr.log"
    printf 'article RGB x=900 y=180 w=2000 h=1650 '
    rg --no-filename -o 'PSNR r:[^ ]+ g:[^ ]+ b:[^ ]+ average:[^ ]+' "$output/article-psnr.log"
    printf 'article gray '
    rg --no-filename -o 'SSIM Y:[^ ]+ \([^)]*\) All:[^ ]+' "$output/article-ssim.log"
    shasum -a 256 "$reference" "$candidate"
} > "$output/metrics.txt"
cat "$output/metrics.txt"
