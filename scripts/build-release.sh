#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
version=$(cat VERSION)
output="$root/dist/$version"
mkdir -p "$output"

for target_os in linux darwin; do
  for target_arch in amd64 arm64; do
    name="tmd-$version-$target_os-$target_arch"
    echo "building $name"
    CGO_ENABLED=0 GOOS=$target_os GOARCH=$target_arch \
      go build -trimpath -ldflags="-s -w -X main.version=$version" \
      -o "$output/$name" ./cmd/tmd
  done
done

cd "$output"
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum tmd-* > SHA256SUMS
else
  shasum -a 256 tmd-* > SHA256SUMS
fi
