#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 3 || ! $1 =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
    printf 'usage: bash scripts/build-update-feed.sh vVERSION DIST HTTPS_ASSET_BASE [os/arch=version=archive ...]\n' >&2
    exit 2
fi
version=${1#v}
dist=$(cd -- "$2" && pwd -P)
base_url=$3
shift 3
: "${VIVA_UPDATE_KEY:?set VIVA_UPDATE_KEY to the Viva Ed25519 private key}"
: "${MARKSTACK_UPDATE_PUBLIC_KEY:?set MARKSTACK_UPDATE_PUBLIC_KEY to the pinned public key}"
cd -- "$(dirname -- "$0")/.."

patches=()
for previous in "$@"; do patches+=(--patch-from "$previous"); done
[[ ! -e "$dist/latest.json" && ! -e "$dist/latest.json.sig" && ! -e "$dist/SHA256SUMS" ]]
shopt -s nullglob
stale=("$dist"/*.patch)
[[ ${#stale[@]} == 0 ]]
go tool viva feed --version "$version" --channel stable --dir "$dist" --base-url "$base_url" \
    --valid-for 8760h --patch-max-ratio 0.6 ${patches[@]+"${patches[@]}"}
go run ./scripts/verify-update-feed.go "$dist/latest.json" "$dist/latest.json.sig"

{
    printf 'Noto Sans SC Regular (PDF export)\n\n'
    cat internal/exportpdf/font/OFL.txt
    printf '\nLucide icons\n\n'
    cat assets/lucide/LICENSE
} > "$dist/THIRD-PARTY-NOTICES.txt"

cd -- "$dist"
shasum -a 256 ./*.zip ./*.exe ./*.tar.gz ./*.deb ./*.patch latest.json latest.json.sig THIRD-PARTY-NOTICES.txt > SHA256SUMS
shasum -a 256 -c SHA256SUMS
