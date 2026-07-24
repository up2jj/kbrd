#!/bin/sh
set -eu

asset_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
module_cache=${TMPDIR:-/tmp}/kbrd-companion-clang-cache

CLANG_MODULE_CACHE_PATH="$module_cache" /usr/bin/clang \
  -fobjc-arc \
  -framework AppKit \
  -framework Carbon \
  -framework UserNotifications \
  -mmacosx-version-min=11.0 \
  -arch arm64 \
  -arch x86_64 \
  "$asset_dir/main.m" \
  -o "$asset_dir/kbrd-companion"

chmod 0644 "$asset_dir/kbrd-companion"
