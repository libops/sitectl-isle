#!/bin/sh
set -eu

minimum="${1:-v0.40.0}"
version="$(GOWORK=off go list -m -f '{{.Version}}' github.com/libops/sitectl)"

case "$version" in
  v*.*.*-*)
    echo "github.com/libops/sitectl $version is a prerelease; release plugins against stable $minimum or newer" >&2
    exit 1
    ;;
esac

first="$(printf '%s\n' "$minimum" "$version" | sort -V | head -n 1)"
if [ "$first" != "$minimum" ]; then
  echo "github.com/libops/sitectl $version is too old; bump go.mod to stable $minimum or newer before releasing" >&2
  exit 1
fi
