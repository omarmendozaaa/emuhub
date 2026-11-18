#!/usr/bin/env bash
# Builds an EmuHub release for one platform.
#
#   scripts/build-release.sh linux-amd64     (on Linux)
#   scripts/build-release.sh windows-amd64   (on Linux or Windows)
#   scripts/build-release.sh darwin-arm64    (on macOS only, Apple Silicon)
#   scripts/build-release.sh darwin-amd64    (on macOS only, Intel)
#
# The version comes from EMUHUB_VERSION or, failing that, from a v* tag on
# the current commit; anything else is a development build, which does not
# update itself.
#
# Output goes to dist/:
#   EmuHub-<platform>            the program (~20 MB). It downloads the
#                                emulators the first time and updates itself.
#   EmuHub-completo-<platform>   the same with the emulators inside, for
#                                installing without downloading them.
#   emuladores-<bundle>-<platform>.zip
#                                only when that emulator package is not
#                                published yet; scripts/publish-release.sh
#                                publishes it.
#
# The emulator package is published once per emulators.lock.json version and
# reused by every later release: the program checks the archive it downloads
# against the SHA-256 embedded at build time, so a build must embed exactly
# the published archive.
set -euo pipefail
cd "$(dirname "$0")/.."

platform="${1:?uso: $0 linux-amd64|windows-amd64|darwin-arm64|darwin-amd64}"
bundle_version="$(sed -n 's/.*"bundleVersion": *"\([^"]*\)".*/\1/p' emulators.lock.json)"
version="${EMUHUB_VERSION:-$(git describe --tags --exact-match --match 'v*' 2>/dev/null || echo "dev-$(git rev-parse --short HEAD)")}"
repo="${EMUHUB_REPO:-omarmendozaaa/emuhub}"

# The macOS emulator builds are universal, so both Mac targets share them.
# Each Mac target is built separately: a universal EmuHub binary would carry
# the embedded emulators twice.
bundle_platform="$platform"
case "$platform" in darwin-*) bundle_platform=darwin-universal ;; esac

# Every target embeds the same internal/bundle/data files and shares
# build/bin, so two builds at once would mix them up.
mkdir -p internal/bundle/data dist
if command -v flock >/dev/null; then
  exec 9>internal/bundle/data/.build.lock
  flock 9
fi

emu_asset="emuladores-$bundle_version-$bundle_platform.zip"
emu_url="https://github.com/$repo/releases/download/emuladores-$bundle_version/$emu_asset"
published="${XDG_CACHE_HOME:-$HOME/.cache}/emuhub-build/published/$emu_asset"
zip=internal/bundle/data/emulators.zip

if [ ! -f "$published" ]; then
  mkdir -p "$(dirname "$published")"
  echo "Buscando el paquete de emuladores publicado: $emu_url"
  code="$(curl -sSL -o "$published.part" -w '%{http_code}' "$emu_url" || true)"
  case "$code" in
    200) mv "$published.part" "$published" ;;
    404) rm -f "$published.part" ;;
    *)
      rm -f "$published.part"
      echo "No se pudo comprobar si el paquete de emuladores está publicado (HTTP $code)." >&2
      echo "Crear uno nuevo daría un archivo distinto del publicado; vuelve a intentarlo." >&2
      exit 1
      ;;
  esac
fi
if [ -f "$published" ]; then
  echo "Usando el paquete de emuladores publicado"
  cp "$published" "$zip"
  rm -f "dist/$emu_asset"
elif [ -f "dist/$emu_asset" ]; then
  # Made by an earlier build that is not published yet (both Mac targets
  # share one): reuse it so they embed the same archive.
  echo "Usando el paquete de emuladores sin publicar de dist/"
  cp "dist/$emu_asset" "$zip"
else
  echo "El paquete $emu_asset no está publicado: se crea"
  go run ./cmd/bundler -platform "$bundle_platform" -out "$zip"
  cp "$zip" "dist/$emu_asset"
fi
go run ./cmd/bundler -describe "$zip" -url "$emu_url"

ldflags="-X main.version=$version -X main.releaseRepo=$repo"
echo "EmuHub $version"
case "$platform" in
  linux-amd64)
    wails build -clean -tags "webkit2_41,bundle" -ldflags "$ldflags" -o emuhub
    cp build/bin/emuhub dist/EmuHub-linux-x86_64
    wails build -s -tags "webkit2_41,bundle,bundlezip" -ldflags "$ldflags" -o emuhub
    cp build/bin/emuhub dist/EmuHub-completo-linux-x86_64
    ;;
  windows-amd64)
    wails build -clean -platform windows/amd64 -tags bundle -ldflags "$ldflags" -o emuhub.exe
    cp build/bin/emuhub.exe dist/EmuHub-windows-x64.exe
    wails build -s -platform windows/amd64 -tags "bundle,bundlezip" -ldflags "$ldflags" -o emuhub.exe
    cp build/bin/emuhub.exe dist/EmuHub-completo-windows-x64.exe
    ;;
  darwin-arm64|darwin-amd64)
    arch="${platform#darwin-}"
    for kind in "" "completo-"; do
      tags=bundle
      [ -n "$kind" ] && tags="bundle,bundlezip"
      clean=-clean
      [ -n "$kind" ] && clean=-s
      wails build $clean -platform "darwin/$arch" -tags "$tags" -ldflags "$ldflags"
      # Ad-hoc signature: unsigned arm64 binaries are refused by macOS outright.
      codesign --force --deep --sign - build/bin/EmuHub.app
      rm -f "dist/EmuHub-${kind}macos-$arch.zip"
      (cd build/bin && ditto -c -k --keepParent EmuHub.app "../../dist/EmuHub-${kind}macos-$arch.zip")
    done
    ;;
  *)
    echo "plataforma desconocida: $platform" >&2
    exit 1
    ;;
esac
ls -lh dist/
