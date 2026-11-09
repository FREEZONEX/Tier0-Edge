#!/usr/bin/env bash
set -euo pipefail
if [[ ${1:-} == --help || $# != 1 ]]; then
  echo 'fetch-linux-native.sh CACHE (Ubuntu 24.04 AMD64 build host)'
  echo 'Build dependencies: build-essential curl ca-certificates git cmake bison flex zlib1g-dev libssl-dev pkg-config xz-utils bzip2 nodejs'
  exit 0
fi
[[ $(uname -s) == Linux && $(uname -m) == x86_64 ]] || { echo 'Linux AMD64 required' >&2; exit 1; }
source /etc/os-release
[[ $ID == ubuntu && $VERSION_ID == 24.04 ]] || { echo 'Ubuntu 24.04 build host required' >&2; exit 1; }
repo=$(cd "$(dirname "$0")/../.." && pwd)
mkdir -p "$1/downloads"
cache=$(cd "$1" && pwd)
node --input-type=module - "$repo/scripts/deploy/linux-native-dependencies.json" "$cache/downloads" <<'JS'
import fs from 'node:fs';import crypto from 'node:crypto';import path from 'node:path';import {execFileSync} from 'node:child_process';
for(const dep of JSON.parse(fs.readFileSync(process.argv[2],'utf8'))){
 const file=path.join(process.argv[3],dep.name);
 if(!fs.existsSync(file)){execFileSync('curl',['-fL','--retry','3','-o',file+'.partial',dep.url],{stdio:'inherit'});fs.renameSync(file+'.partial',file);}
 if(crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex')!==dep.sha256)throw Error('Checksum mismatch: '+dep.name);
}
JS
if [[ -f $cache/dependencies-ready.json ]]; then
  cmp "$cache/dependencies-ready.json" "$repo/scripts/deploy/linux-native-dependencies.json"
  exit 0
fi
for item in postgres node-v22.23.1-linux-x64 nats-server-v2.15.2-linux-amd64; do
  [[ ! -e $cache/$item ]] || { echo "Partial cache retained: $item; use a new cache" >&2; exit 1; }
done
build=$(mktemp -d)
echo "Dependency build evidence: $build"
tar -xf "$cache/downloads/node-v22.23.1-linux-x64.tar.xz" -C "$cache"
tar -xf "$cache/downloads/nats-server-v2.15.2-linux-amd64.tar.gz" -C "$cache"
tar -xf "$cache/downloads/postgresql-17.11.tar.bz2" -C "$build"
tar -xf "$cache/downloads/timescaledb-2.20.0.tar.gz" -C "$build"
jobs=${BUILD_JOBS:-4}
(
  cd "$build/postgresql-17.11"
  ./configure --prefix="$cache/postgres" --datadir="$cache/postgres/share/postgresql" --without-icu --without-readline --with-ssl=openssl
  make -j"$jobs"
  make install
  make -C contrib/pg_stat_statements -j"$jobs"
  make -C contrib/pg_stat_statements install
)
(
  cd "$build/timescaledb-2.20.0"
  ./bootstrap -DPG_CONFIG="$cache/postgres/bin/pg_config" -DREGRESS_CHECKS=OFF -DWARNINGS_AS_ERRORS=OFF
  cmake --build build -j"$jobs"
  cmake --install build
)
mkdir -p "$cache/licenses"
cp "$build/postgresql-17.11/COPYRIGHT" "$cache/licenses/PostgreSQL-COPYRIGHT.txt"
cp "$build/timescaledb-2.20.0/LICENSE" "$cache/licenses/TimescaleDB-LICENSE.txt"
cp "$build/timescaledb-2.20.0/tsl/LICENSE-TIMESCALE" "$cache/licenses/TimescaleDB-TSL-LICENSE.txt"
cp "$repo/scripts/deploy/linux-native-dependencies.json" "$cache/dependencies-ready.json"
echo "Dependencies ready: $cache"
