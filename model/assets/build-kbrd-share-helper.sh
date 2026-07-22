#!/bin/sh
set -eu

asset_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
module_cache=${TMPDIR:-/tmp}/kbrd-clang-module-cache

CLANG_MODULE_CACHE_PATH="$module_cache" /usr/bin/clang \
	-fobjc-arc \
	-framework AppKit \
	-mmacosx-version-min=11.0 \
	-arch arm64 \
	-arch x86_64 \
	"$asset_dir/kbrd-share.m" \
	-o "$asset_dir/kbrd-share-helper"

chmod 0644 "$asset_dir/kbrd-share-helper"
