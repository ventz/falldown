#!/usr/bin/env bash
# Downloads the original Fall Down releases from ticalc.org into original/,
# checks them against known SHA-256 sums, and unpacks them. The files belong
# to their authors and are not part of this repository.
set -euo pipefail

base="https://www.ticalc.org/pub/83plus/asm/games/mirageos"
root="$(cd "$(dirname "$0")/.." && pwd)/original"

# dir|file|sha256
archives=(
  "v1.5-ion-mirageos|fdown.zip|396feaec4800d3faf084136b79faa11c2fb0aa44ffa2ef24cbf8d8c3c55b99f4"
  "v1.65-multiplayer|fdownm.zip|bc9e717076664799e8feb5e29d23f3be62706ea6cb11b6d621c16eae2a834360"
)

sha256() {
  if command -v sha256sum >/dev/null; then
    sha256sum "$1" | cut -d' ' -f1
  else
    shasum -a 256 "$1" | cut -d' ' -f1
  fi
}

for entry in "${archives[@]}"; do
  IFS='|' read -r dir file want <<<"$entry"
  dest="$root/$dir"
  mkdir -p "$dest"
  if [[ ! -f "$dest/$file" ]] || [[ "$(sha256 "$dest/$file")" != "$want" ]]; then
    echo "downloading $base/$file"
    curl -fsSL -o "$dest/$file.part" "$base/$file"
    got="$(sha256 "$dest/$file.part")"
    if [[ "$got" != "$want" ]]; then
      echo "checksum mismatch for $file: got $got, want $want" >&2
      rm -f "$dest/$file.part"
      exit 1
    fi
    mv -f "$dest/$file.part" "$dest/$file"
  fi
  unzip -oq "$dest/$file" -d "$dest"
  echo "$dir: $(cd "$dest" && ls | tr '\n' ' ')"
done
