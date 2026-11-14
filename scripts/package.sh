#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 || "$1" != "--no-sign" ]]; then
    printf 'usage: bash scripts/package.sh --no-sign\n' >&2
    exit 2
fi

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
go tool viva package --no-sign
app="$root/build/MarkStack.app"
license="$root/internal/exportpdf/font/OFL.txt"
install -m 0644 "$license" "$app/Contents/Resources/PDF-Font-OFL.txt"
cmp -s "$license" "$app/Contents/Resources/PDF-Font-OFL.txt"
printf 'font license  %s\n' "$app/Contents/Resources/PDF-Font-OFL.txt"
icon_license="$root/assets/lucide/LICENSE"
install -m 0644 "$icon_license" "$app/Contents/Resources/Lucide-LICENSE.txt"
cmp -s "$icon_license" "$app/Contents/Resources/Lucide-LICENSE.txt"
printf 'icon license  %s\n' "$app/Contents/Resources/Lucide-LICENSE.txt"
