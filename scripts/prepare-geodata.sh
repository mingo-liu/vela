#!/bin/sh
set -eu

# MetaCubeX/meta-rules-dat release branch at a fixed commit.
checksum=21606dfffd4ec39542ec40782bebd37b71310471816e1338b6f6430190cbc85d
geosite_checksum=12dfa24f466986cfe1ead0c67e554e5c67642bb6b204ea64172178d25825f216
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

geosite="$resources/geosite.dat"
if [ ! -f "$geosite" ]; then
  curl --fail --location --silent --show-error --output "$geosite.tmp" \
    "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/6b01e65c89cdeb684c00c70bb0b414091b22f124/geosite.dat"
  mv "$geosite.tmp" "$geosite"
fi

actual=$(shasum -a 256 "$geosite" | cut -d ' ' -f 1)
if [ "$actual" != "$geosite_checksum" ]; then
  echo "GeoSite database checksum mismatch: expected $geosite_checksum, got $actual" >&2
  exit 1
fi
