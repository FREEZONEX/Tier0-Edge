#!/usr/bin/env bash
set -euo pipefail
if [[ ${1:-} == --help || $# -lt 1 || $# -gt 2 ]]; then
  echo 'Usage: prepare-macos-cross-cache.sh NEW_CACHE [TIER0_EDGE_REFERENCE_DMG]'
  exit 0
fi
[[ $(uname -s)/$(uname -m) == Linux/x86_64 ]] || { echo 'Linux x64 required' >&2; exit 1; }
repo=$(cd "$(dirname "$0")/../.." && pwd)
cache=$1
reference=${2:-}
[[ ! -e "$cache" && ! -L "$cache" ]] || { echo 'Cache directory must be new' >&2; exit 1; }
mkdir -p "$(dirname "$cache")"
cache=$(realpath -m "$cache")
work=$(mktemp -d "$(dirname "$cache")/.macos-cross.XXXXXX")
inputs="$work/inputs"
output="$work/cache"
mkdir -p "$inputs" "$output/licenses" "$output/artwork"
# The pinned native DMG supplies prebuilt PostgreSQL/TimescaleDB and design
# assets. All Tier0 executables are rebuilt from the selected source checkout.
ref_version=3.0.0-024913
ref_name=Tier0-Edge-$ref_version-macOS-arm64.dmg
volume="Tier0 Edge $ref_version"
if [[ -z $reference ]]; then
  reference="$inputs/$ref_name"
  if ! curl -fL --connect-timeout 10 -o "$reference" "https://github.com/FREEZONEX/Tier0-Edge/releases/download/v3.0.0/$ref_name"; then
    curl -fL --retry 3 -H 'Accept: application/octet-stream' -o "$reference" \
      'https://api.github.com/repos/FREEZONEX/Tier0-Edge/releases/assets/726650518?binary=1'
  fi
fi
printf '%s  %s\n' f8840c65c052dd1e29305141acd43e971117033ae02d93a395877a145546900b "$reference" | sha256sum -c
node --input-type=module - "$repo/scripts/deploy/macos-native-dependencies.json" "$inputs" <<'JS'
import fs from 'node:fs';
import crypto from 'node:crypto';
import path from 'node:path';
import {execFileSync} from 'node:child_process';
const names = new Set(['node-mac.tar.gz', 'nats-mac.tar.gz']);
for (const dep of JSON.parse(fs.readFileSync(process.argv[2], 'utf8'))) {
  if (!names.has(dep.name)) continue;
  const file = path.join(process.argv[3], dep.name);
  execFileSync('curl', ['-fL', '--retry', '3', '-o', file, dep.url.startsWith('https://github.com/') ? dep.url+'?download=1' : dep.url], {stdio: 'inherit'});
  if (crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex') !== dep.sha256) {
    throw Error('Checksum mismatch: ' + dep.name);
  }
}
JS
resources="$volume/Tier0 Edge.app/Contents/Resources"
runtime="$resources/runtime"
7z x -y -sns -o"$inputs/dmg" "$reference" "$runtime/postgres/*" "$runtime/licenses/PostgresApp-Credits.rtf" \
  "$runtime/licenses/TimescaleDB-LICENSE.txt" "$runtime/licenses/TimescaleDB-TSL-LICENSE.txt" \
  "$resources/Tier0Edge.icns" "$resources/favicon.ico" "$volume/.background/background.png" "$volume/Applications" >/dev/null
mv "$inputs/dmg/$runtime/postgres" "$output/postgres"
cp "$inputs/dmg/$runtime/licenses/"* "$output/licenses/"
cp "$inputs/dmg/$resources/"{Tier0Edge.icns,favicon.ico} "$output/artwork/"
cp "$inputs/dmg/$volume/.background/background.png" "$output/artwork/"
cp "$inputs/dmg/$volume/Applications" "$output/artwork/"
cp "$inputs/dmg/$volume/Applications:rsrc" "$output/artwork/Applications:com.apple.ResourceFork"
for name in node-mac.tar.gz nats-mac.tar.gz; do tar -xzf "$inputs/$name" -C "$output"; done
(cd "$output"; find . -type f -print0 | sort -z | xargs -0 sha256sum > "$work/SHA256SUMS")
mv "$work/SHA256SUMS" "$output/"
mv -T --no-clobber "$output" "$cache"
[[ ! -e "$output" ]] || { echo 'Cache was created concurrently' >&2; exit 1; }
echo "Cross-build inputs ready: $cache"
