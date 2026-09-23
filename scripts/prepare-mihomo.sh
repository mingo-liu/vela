#!/bin/sh
set -eu

version=v1.19.31
name="mihomo-darwin-arm64-${version}.gz"
checksum=d131f44b3deb2a8356f7ac75048ad67a10d53243323951c4f3cda7b672922963
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
resources="$root/build/resources"
archive="$resources/$name"
mkdir -p "$resources"

if [ ! -f "$archive" ]; then
  curl --fail --location --silent --show-error --output "$archive.tmp" \
    "https://github.com/MetaCubeX/mihomo/releases/download/${version}/${name}"
  mv "$archive.tmp" "$archive"
fi

actual=$(shasum -a 256 "$archive" | cut -d ' ' -f 1)
if [ "$actual" != "$checksum" ]; then
  echo "mihomo checksum mismatch: expected $checksum, got $actual" >&2
  exit 1
fi

gzip -dc "$archive" > "$resources/mihomo.tmp"
chmod 0755 "$resources/mihomo.tmp"
mv "$resources/mihomo.tmp" "$resources/mihomo"
