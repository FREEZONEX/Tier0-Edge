#!/usr/bin/env bash
set -euo pipefail
if [[ ${1:-} == --help || $# != 1 ]]; then
  echo 'Usage: verify-macos-cross-dmg.sh DMG (with SHA256SUMS in the same directory)'
  exit 0
fi
[[ $(uname -s)/$(uname -m) == Darwin/arm64 ]] || { echo 'Verify on Apple Silicon' >&2; exit 1; }
dmg="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
(cd "$(dirname "$dmg")"; shasum -a 256 -c SHA256SUMS)
hdiutil verify "$dmg"
mount=$(mktemp -d "${TMPDIR:-/tmp}/tier0-cross-dmg.XXXXXX")
cleanup() {
  hdiutil detach "$mount" >/dev/null 2>&1 || true
  rmdir "$mount"
}
trap cleanup EXIT
hdiutil attach -readonly -nobrowse -noautoopen -mountpoint "$mount" "$dmg" >/dev/null
[[ -s "$mount/.DS_Store" && -s "$mount/.background/background.png" ]]
swift - "$mount/Applications" <<'SWIFT'
import Foundation
let alias = URL(fileURLWithPath: CommandLine.arguments[1])
let destination = try URL(resolvingAliasFileAt: alias, options: [.withoutUI, .withoutMounting])
let values = try alias.resourceValues(forKeys: [.isAliasFileKey])
guard destination.path == "/Applications", values.isAliasFile == true,
      let icon = FileHandle(forReadingAtPath: alias.path + "/..namedfork/rsrc"),
      try icon.seekToEnd() > 0 else {
    fatalError("Applications must be a Finder alias with an embedded icon")
}
try icon.close()
print("Verified Applications Finder alias and custom icon")
SWIFT
app="$mount/Tier0 Edge.app"
codesign --verify --deep --strict "$app"
runtime="$app/Contents/Resources/runtime"
"$runtime/node/node" --input-type=module - "$runtime" <<'JS'
import assert from 'node:assert/strict';
import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import {execFileSync} from 'node:child_process';
const root = process.argv[2];
const manifest = JSON.parse(fs.readFileSync(path.join(root,'package.json'),'utf8'));
const distribution = manifest.distribution;
assert.equal(distribution.edition, 'edge');
assert.equal(distribution.platform, 'darwin/arm64');
assert.equal(distribution.deployment, 'native');
assert.equal(manifest.sourceDirty, false);
const run = (file, arg) => execFileSync(path.join(root,file),[arg],{encoding:'utf8',timeout:15000}).trim();
for (const file of ['native-launcher', ...distribution.components.filter(c => ['backend','fleet','executor','modbus'].includes(c.id)).map(c => c.artifact)]) {
  const actual = JSON.parse(run(file,'--product-identity'));
  for (const key of ['edition','platform','deployment','productVersion','codeVersion']) assert.equal(actual[key], distribution[key], file);
}
for (const c of distribution.components.filter(c => ['postgres','nats','node'].includes(c.id))) {
  const version = run(c.artifact, '--version');
  assert.equal(c.id === 'node' ? version.replace(/^v/,'') : version, c.version, c.artifact);
}
for (const [file,sha256] of Object.entries(manifest.files)) {
  assert.equal(crypto.createHash('sha256').update(fs.readFileSync(path.join(root,file))).digest('hex'),sha256,file);
}
console.log('Verified target identities and', Object.keys(manifest.files).length, 'payload files for', manifest.packageVersion);
JS
