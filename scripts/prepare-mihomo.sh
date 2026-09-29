#!/bin/sh
set -eu

# Build the pinned release with reviewed dependency updates. Upstream's release
# binary retains vulnerable dependencies to support older Go toolchains.
revision=ab405bad5beeeac8b003bb01f60f134f6df54471
checksum=adb7a6e4207e8ca44567330e1d563163112787d3044d848305b2bfeff0282590
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
resources="$root/build/resources"
archive="$resources/mihomo-source-$revision.tar.gz"
mkdir -p "$resources"

if [ ! -f "$archive" ]; then
  curl --fail --location --silent --show-error --output "$archive.tmp" \
    "https://codeload.github.com/MetaCubeX/mihomo/tar.gz/$revision"
  mv "$archive.tmp" "$archive"
fi
actual=$(shasum -a 256 "$archive" | cut -d ' ' -f 1)
if [ "$actual" != "$checksum" ]; then
  echo "mihomo source checksum mismatch: expected $checksum, got $actual" >&2
  exit 1
fi

# Include the build recipe and lockfiles in the cache key. Also verify the
# cached executable, so a stale or replaced binary is rebuilt.
recipe=$(cat "$0" "$root/scripts/mihomo/go.mod" "$root/scripts/mihomo/go.sum" | shasum -a 256 | cut -d ' ' -f 1)
if [ -x "$resources/mihomo" ] && [ -f "$resources/mihomo.build" ]; then
  binary_hash=$(shasum -a 256 "$resources/mihomo" | cut -d ' ' -f 1)
  if [ "$(cat "$resources/mihomo.build")" = "$recipe $binary_hash" ]; then
    exit 0
  fi
fi

work=$(mktemp -d "$resources/.mihomo-build-XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM
mkdir "$work/source"
tar -xzf "$archive" -C "$work/source" --strip-components=1
cp "$root/scripts/mihomo/go.mod" "$work/source/go.mod"
cp "$root/scripts/mihomo/go.sum" "$work/source/go.sum"
(
  cd "$work/source"
  # Retain symbols so binary scans distinguish linked packages from unused modules.
  CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 GOTOOLCHAIN=go1.26.8 go build \
    -mod=readonly -buildvcs=false -tags with_gvisor -trimpath \
    -ldflags '-buildid= -X github.com/metacubex/mihomo/constant.Version=v1.19.31-vela.1 -X github.com/metacubex/mihomo/constant.BuildTime=2026-09-29' \
    -o "$work/mihomo" .
)
chmod 0755 "$work/mihomo"
binary_hash=$(shasum -a 256 "$work/mihomo" | cut -d ' ' -f 1)
printf '%s %s\n' "$recipe" "$binary_hash" > "$work/mihomo.build"
mv "$work/mihomo" "$resources/mihomo"
mv "$work/mihomo.build" "$resources/mihomo.build"
