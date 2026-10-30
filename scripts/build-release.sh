#!/usr/bin/env bash
# Builds a single-file EmuHub release with the emulators embedded.
#
#   scripts/build-release.sh linux-amd64     (on Linux)
#   scripts/build-release.sh windows-amd64   (on Linux or Windows)
#   scripts/build-release.sh darwin-arm64     (on macOS only, Apple Silicon)
#   scripts/build-release.sh darwin-amd64     (on macOS only, Intel)
#
# Output goes to dist/.
set -euo pipefail
cd "$(dirname "$0")/.."

platform="${1:?uso: $0 linux-amd64|windows-amd64|darwin-arm64|darwin-amd64}"
version="$(sed -n 's/.*"bundleVersion": *"\([^"]*\)".*/\1/p' emulators.lock.json)"

# The macOS emulator builds are universal, so both Mac targets share them.
# Each Mac target is built separately: a universal EmuHub binary would carry
# the embedded emulators twice.
bundle_platform="$platform"
case "$platform" in darwin-*) bundle_platform=darwin-universal ;; esac

go run ./cmd/bundler -platform "$bundle_platform" -out internal/bundle/data/emulators.zip

mkdir -p dist
case "$platform" in
  linux-amd64)
    wails build -clean -tags "webkit2_41,bundle" -o emuhub
    cp build/bin/emuhub "dist/EmuHub-$version-linux-x86_64"
    ;;
  windows-amd64)
    wails build -clean -platform windows/amd64 -tags bundle -o emuhub.exe
    cp build/bin/emuhub.exe "dist/EmuHub-$version-windows-x64.exe"
    ;;
  darwin-arm64|darwin-amd64)
    arch="${platform#darwin-}"
    wails build -clean -platform "darwin/$arch" -tags bundle
    # Ad-hoc signature: unsigned arm64 binaries are refused by macOS outright.
    codesign --force --deep --sign - build/bin/EmuHub.app
    (cd build/bin && ditto -c -k --keepParent EmuHub.app "../../dist/EmuHub-$version-macos-$arch.zip")
    ;;
  *)
    echo "plataforma desconocida: $platform" >&2
    exit 1
    ;;
esac
ls -lh dist/
