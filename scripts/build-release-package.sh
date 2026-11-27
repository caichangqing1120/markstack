#!/usr/bin/env bash
set -euo pipefail

if [[ $# != 4 || ! $1 =~ ^(darwin|windows|linux)$ || ! $2 =~ ^(amd64|arm64)$ || ! $3 =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
    printf 'usage: bash scripts/build-release-package.sh OS ARCH vVERSION OUTPUT\n' >&2
    exit 2
fi
target=$1 arch=$2 version=${3#v}
[[ ${MARKSTACK_UPDATE_PUBLIC_KEY:-} =~ ^[A-Za-z0-9+/]{43}=$ ]]
[[ ${MARKSTACK_UPDATE_FEED_URL:-} =~ ^https://[A-Za-z0-9./_-]+/latest\.json$ ]]

mkdir -p -- "$4"
out=$(cd -- "$4" && pwd -P)
root=$(cd -- "$(dirname -- "$0")/.." && pwd -P)
cd "$root"
commit=$(git rev-parse HEAD)
scratch=$(mktemp -d "${TMPDIR:-/tmp}/markstack-package.XXXXXX")
trap 'rm -rf -- "$scratch"' EXIT
mkdir -p "$scratch/assets"
cp assets/markstack-icon.png "$scratch/assets/"
awk -v version="$version" '
    /^\[app\]$/ { app=1; print; printf "version = \"%s\"\n", version; next }
    /^\[/ { app=0 }
    app && /^[[:space:]]*version[[:space:]]*=/ { next }
    { print }
' viva.toml > "$scratch/viva.toml"

flags="-s -w -buildid= -X markstack/internal/buildinfo.Version=$version -X markstack/internal/buildinfo.Commit=$commit -X markstack/internal/buildinfo.Channel=stable -X markstack/internal/buildinfo.FeedURL=$MARKSTACK_UPDATE_FEED_URL -X markstack/internal/buildinfo.PublicKey=$MARKSTACK_UPDATE_PUBLIC_KEY"
program="$scratch/markstack"
cgo=0
if [[ $target == darwin ]]; then
    cgo=1
    export CGO_CFLAGS=-mmacosx-version-min=12.0 CGO_LDFLAGS=-mmacosx-version-min=12.0
elif [[ $target == windows ]]; then
    program+=.exe
    flags+=' -H=windowsgui'
fi
GOOS="$target" GOARCH="$arch" CGO_ENABLED="$cgo" go build -trimpath -ldflags "$flags" -o "$program" ./cmd/markstack

package=(go tool viva package -C "$scratch" --target "$target" --arch "$arch" --program "$program")
case $target in
    darwin)
        signing=(--notarize)
        if [[ ${MARKSTACK_UNSIGNED_DEVELOPMENT:-} == 1 ]]; then signing=(--no-sign); fi
        "${package[@]}" --format zip --out "$out/MarkStack.app" "${signing[@]}"
        ;;
    windows)
        go tool viva sign --target windows --program "$program"
        "${package[@]}" --format nsis,zip --out "$out" \
            --file "$root/internal/exportpdf/font/OFL.txt=PDF-Font-OFL.txt" \
            --file "$root/assets/lucide/LICENSE=Lucide-LICENSE.txt"
        ;;
    linux)
        "${package[@]}" --format tar.gz,deb --out "$out" --no-sign
        ;;
esac
