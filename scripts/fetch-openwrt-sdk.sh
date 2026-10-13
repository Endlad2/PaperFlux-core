#!/usr/bin/env bash
# Download and verify the OpenWRT SDK used to cross-compile PaperFlux for armsr/armv8.
#
# Usage:
#   scripts/fetch-openwrt-sdk.sh [destination]
#
# Default destination is <repo_root>/.cache/openwrt-sdk, where repo_root is the
# parent of this script's directory.
#
# Exports (when sourced or when called with --print-root):
#   sdk_root  -> the extracted SDK directory
set -euo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
sdk_name=openwrt-sdk-25.12.5-armsr-armv8_gcc-14.3.0_musl.Linux-x86_64
archive=$sdk_name.tar.zst
url=https://downloads.openwrt.org/releases/25.12.5/targets/armsr/armv8/$archive
expected_sha256=1b0316604a3e820b2b008a1baff3f9dac6716af942bef800930e58c7de98c98b
destination=${1:-$repo_root/.cache/openwrt-sdk}
sdk_root=$destination/$sdk_name
archive_path=$destination/$archive

mkdir -p "$destination"

if [ ! -d "$sdk_root" ]; then
  if [ ! -f "$archive_path" ]; then
    echo "Downloading $url"
    curl -fL --retry 5 --retry-delay 5 -o "$archive_path" "$url"
  fi

  actual_sha256=$(sha256sum "$archive_path" | awk '{print $1}')
  if [ "$actual_sha256" != "$expected_sha256" ]; then
    echo "SHA256 mismatch for $archive_path" >&2
    echo "  expected: $expected_sha256" >&2
    echo "  actual:   $actual_sha256" >&2
    rm -f "$archive_path"
    exit 1
  fi

  echo "Extracting $archive_path"
  tar --zstd -xf "$archive_path" -C "$destination"
fi

if [ ! -d "$sdk_root" ]; then
  echo "SDK directory not found after extraction: $sdk_root" >&2
  exit 1
fi

echo "sdk_root=$sdk_root"
