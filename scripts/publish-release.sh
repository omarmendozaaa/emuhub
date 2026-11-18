#!/usr/bin/env bash
# Publishes what scripts/build-release.sh left in dist/ to the GitHub
# release of a version tag, creating it if needed:
#
#   scripts/publish-release.sh v0.2.0
#
# CI runs it for Linux and Windows. macOS is built by hand: build on the Mac
# and run this script there with the same tag, and its files join the
# release (latest.json keeps the platforms already published).
#
# New emulator packages go to their own release, emuladores-<version>,
# marked as a prerelease so it never becomes the "latest" one the programs
# update from. They are published once and never replaced: every program
# already released checks their SHA-256.
#
# Needs gh logged in (GH_TOKEN in CI).
set -euo pipefail
cd "$(dirname "$0")/.."

tag="${1:?uso: $0 vX.Y.Z}"
repo="${EMUHUB_REPO:-omarmendozaaa/emuhub}"
export GH_REPO="$repo"

shopt -s nullglob

for f in dist/emuladores-*.zip; do
  name="$(basename "$f")"
  rest="${name#emuladores-}"
  rest="${rest%.zip}"            # <bundle>-<os>-<arch>
  bundle_version="${rest%-*-*}"
  etag="emuladores-$bundle_version"
  if ! gh release view "$etag" >/dev/null 2>&1; then
    gh release create "$etag" --prerelease --latest=false --title "Emuladores $bundle_version" \
      --notes "Emuladores que EmuHub descarga solo la primera vez. No hace falta descargarlos a mano."
  fi
  digest="$(gh api "repos/$repo/releases/tags/$etag" --jq ".assets[] | select(.name == \"$name\") | (.digest // \"sin-digest\")" || true)"
  if [ "$digest" = "sin-digest" ]; then
    echo "$name ya está publicado (GitHub no da su suma para comprobarlo)"
  elif [ -n "$digest" ]; then
    local_sum="sha256:$( (sha256sum "$f" 2>/dev/null || shasum -a 256 "$f") | cut -d' ' -f1)"
    if [ "$digest" != "$local_sum" ]; then
      echo "ERROR: $name ya está publicado con otro contenido ($digest)." >&2
      echo "Los programas de este build no podrían descargarlo. Borra dist/ y la caché" >&2
      echo "~/.cache/emuhub-build/published y vuelve a compilar: usará el publicado." >&2
      exit 1
    fi
    echo "$name ya está publicado"
  else
    echo "Publicando $name"
    gh release upload "$etag" "$f"
  fi
done

programs=()
for f in dist/EmuHub-linux-x86_64 dist/EmuHub-windows-x64.exe dist/EmuHub-macos-*.zip \
         dist/EmuHub-completo-linux-x86_64 dist/EmuHub-completo-windows-x64.exe dist/EmuHub-completo-macos-*.zip; do
  programs+=("$f")
done
if [ ${#programs[@]} -eq 0 ]; then
  echo "No hay programas en dist/" >&2
  exit 1
fi

if ! gh release view "$tag" >/dev/null 2>&1; then
  gh release create "$tag" --verify-tag --generate-notes --title "EmuHub $tag"
fi
old="$(mktemp -d)"
trap 'rm -rf "$old"' EXIT
gh release download "$tag" -p latest.json -D "$old" 2>/dev/null || true
go run ./cmd/releaser -version "$tag" -merge "$old/latest.json" -out dist/latest.json "${programs[@]}"
# latest.json goes last: a program only finds the update once its file is up.
gh release upload "$tag" --clobber "${programs[@]}"
gh release upload "$tag" --clobber dist/latest.json
