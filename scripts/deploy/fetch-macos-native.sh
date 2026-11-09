#!/usr/bin/env bash
set -euo pipefail
if [[ ${1:-} == --help || $# != 1 ]]; then
  echo 'Usage: fetch-macos-native.sh CACHE (Apple Silicon, Xcode and Node required)'
  exit 0
fi
[[ $(uname -s) == Darwin && $(uname -m) == arm64 ]] || { echo 'Darwin ARM64 required'; exit 1; }
repo=$(cd "$(dirname "$0")/../.." && pwd)
mkdir -p "$1"
cache=$(cd "$1" && pwd)
node --input-type=module - "$repo/scripts/deploy/macos-native-dependencies.json" "$cache" <<'JS'
import fs from 'node:fs';import crypto from 'node:crypto';import path from 'node:path';import {execFileSync} from 'node:child_process';
for(const dep of JSON.parse(fs.readFileSync(process.argv[2],'utf8'))){
 const file=path.join(process.argv[3],dep.name);
 if(!fs.existsSync(file)){execFileSync('curl',['-fL','--retry','3','-o',file+'.partial',dep.url],{stdio:'inherit'});fs.renameSync(file+'.partial',file);}
 if(crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex')!==dep.sha256)throw Error('Checksum mismatch: '+dep.name);
}
JS
cd "$cache"
if [[ ! -f dependencies-ready.json ]]; then
  for target in postgres timescaledb-2.20.0 node-v22.23.1-darwin-arm64 nats-server-v2.15.2-darwin-arm64 cmake-3.31.6-macos-universal; do
    [[ ! -e $target ]] || { echo "Partial cache retained: $target; use a new cache directory or diagnose before retrying"; exit 1; }
  done
  mkdir -p pg-mount
  hdiutil attach -nobrowse -mountpoint "$cache/pg-mount" postgres-mac17.dmg
  trap 'hdiutil detach "$cache/pg-mount" >/dev/null 2>&1 || true' EXIT
  ditto pg-mount/Postgres.app/Contents/Versions/17 postgres
  cp pg-mount/Postgres.app/Contents/Resources/Credits.rtf PostgresApp-Credits.rtf
  hdiutil detach "$cache/pg-mount"
  trap - EXIT
  for archive in timescale-mac.tar.gz node-mac.tar.gz nats-mac.tar.gz cmake-mac.tar.gz; do tar xzf "$archive"; done
  export PATH="$cache/cmake-3.31.6-macos-universal/CMake.app/Contents/bin:$PATH"
  cd timescaledb-2.20.0
  ./bootstrap -DPG_CONFIG="$cache/postgres/bin/pg_config" -DREGRESS_CHECKS=OFF -DWARNINGS_AS_ERRORS=OFF -DCMAKE_C_FLAGS="-I$cache/postgres/include" -DCMAKE_MODULE_LINKER_FLAGS="-L$cache/postgres/lib"
  cmake --build build -j4
  cmake --install build
  cp "$repo/scripts/deploy/macos-native-dependencies.json" "$cache/dependencies-ready.json"
fi
echo "Dependencies ready: $cache"
