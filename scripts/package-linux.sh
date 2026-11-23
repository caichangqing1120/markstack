#!/usr/bin/env bash
set -euo pipefail
umask 022
export LC_ALL=C

if [[ $# -ne 2 || $1 != --no-sign || ( $2 != arm64 && $2 != amd64 ) ]]; then
    printf 'usage: bash scripts/package-linux.sh --no-sign {arm64|amd64}\n' >&2
    exit 2
fi
if [[ $(uname -s) != Linux ]] || ! command -v dpkg-deb >/dev/null; then
    printf 'Linux and dpkg-deb are required to build the developer package.\n' >&2
    exit 1
fi

root=$(cd -- "$(dirname -- "$0")/.." && pwd -P)
cd "$root"
mkdir -p build
[[ ! -L build ]]
staging=$(mktemp -d "$root/build/.markstack-linux.XXXXXX")
trap 'find "$staging" -depth -delete' EXIT
export SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-0}

go tool viva package --target linux --arch "$2" --format deb --no-sign --out "$staging/stock"
packages=("$staging/stock"/*.deb)
[[ ${#packages[@]} == 1 && -f ${packages[0]} ]]
package=${packages[0]}
[[ $(dpkg-deb -f "$package" Package) == markstack && $(dpkg-deb -f "$package" Architecture) == "$2" ]]
payload="$staging/payload"
dpkg-deb -R "$package" "$payload"

notice=usr/share/doc/markstack/copyright
[[ ! -e "$payload/$notice" && ! -L "$payload/$notice" ]]
install -d -m 0755 "$(dirname -- "$payload/$notice")"
{
    printf 'Third-party notices for bundled MarkStack assets\n\nNoto Sans SC Regular (PDF export)\n\n'
    cat internal/exportpdf/font/OFL.txt
    printf '\nLucide icons\n\n'
    cat assets/lucide/LICENSE
} > "$payload/$notice"
(
    cd "$payload"
    md5sum -c --quiet DEBIAN/md5sums
    md5sum "$notice" >> DEBIAN/md5sums
)
notice_bytes=$(wc -c < "$payload/$notice")
awk -v size="$(( (notice_bytes + 1023) / 1024 ))" '
  /^Installed-Size: [0-9]+$/ { print "Installed-Size: " ($2 + size); changed++; next }
  { print }
  END { exit changed != 1 }
' "$payload/DEBIAN/control" > "$staging/control"
mv -- "$staging/control" "$payload/DEBIAN/control"

output="build/${package##*/}"
[[ ! -L "$output" && ( ! -e "$output" || -f "$output" ) ]]
dpkg-deb --root-owner-group --build "$payload" "$staging/final.deb"
mv -fT -- "$staging/final.deb" "$output"
printf 'Unsigned Linux development package: %s\n' "$output"
