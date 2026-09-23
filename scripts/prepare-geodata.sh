#!/bin/sh
set -eu

# MetaCubeX/meta-rules-dat release branch at a fixed commit.
checksum=21606dfffd4ec39542ec40782bebd37b71310471816e1338b6f6430190cbc85d
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
resources="$root/build/resources"
database="$resources/Country.mmdb"
mkdir -p "$resources"

if [ ! -f "$database" ]; then
  curl --fail --location --silent --show-error --output "$database.tmp" \
    "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/6b01e65c89cdeb684c00c70bb0b414091b22f124/country.mmdb"
  mv "$database.tmp" "$database"
fi

actual=$(shasum -a 256 "$database" | cut -d ' ' -f 1)
if [ "$actual" != "$checksum" ]; then
  echo "GeoIP database checksum mismatch: expected $checksum, got $actual" >&2
  exit 1
fi
