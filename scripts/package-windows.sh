#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 || $# -gt 3 || $1 != --no-sign || ( $2 != amd64 && $2 != arm64 ) || ( $# == 3 && $3 != --portable-only ) ]]; then
    printf 'usage: bash scripts/package-windows.sh --no-sign {amd64|arm64} [--portable-only]\n' >&2
    exit 2
fi

root=$(cd -- "$(dirname -- "$0")/.." && pwd -P)
cd "$root"
mkdir -p build
[[ ! -L build ]]
formats=nsis,zip
if [[ ${3:-} == --portable-only ]]; then
    formats=zip
fi

go tool viva package --target windows --arch "$2" --format "$formats" --no-sign --out build \
    --file internal/exportpdf/font/OFL.txt=PDF-Font-OFL.txt \
    --file assets/lucide/LICENSE=Lucide-LICENSE.txt
