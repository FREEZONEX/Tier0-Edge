#!/usr/bin/env bash
set -euo pipefail
if [[ ${1:-} == --help || $# -lt 3 ]]; then
  echo 'package-linux-native.sh CACHE NEW_OUTPUT WEB_DIST [PREBUILT_BIN]'
  exit 0
fi
[[ $(uname -s) == Linux && $(uname -m) == x86_64 ]] || { echo 'Linux AMD64 required' >&2; exit 1; }
source /etc/os-release
[[ $ID == ubuntu && $VERSION_ID == 24.04 ]] || { echo 'Build this package on Ubuntu 24.04' >&2; exit 1; }
repo=$(cd "$(dirname "$0")/../.." && pwd)
cache=$(cd "$1" && pwd)
out=$2
web=$(cd "$3" && pwd)
[[ ! -e $out && ! -L $out ]] || { echo 'Output must be new' >&2; exit 1; }
cmp "$cache/dependencies-ready.json" "$repo/scripts/deploy/linux-native-dependencies.json"
export PATH="$cache/node-v22.23.1-linux-x64/bin:$PATH"
version=$(node "$repo/scripts/deploy/product-package.mjs" plan edge linux/amd64 native | node -p 'JSON.parse(require("fs").readFileSync(0,"utf8")).productVersion')
revision=$(git -C "$repo" rev-parse HEAD)
mkdir -p "$out"
out=$(cd "$out" && pwd)
payload="$out/payload"
mkdir -p "$payload/backend" "$payload/node/node_modules" "$payload/nats" "$payload/licenses"
if [[ $# -gt 3 ]]; then bins=$(cd "$4" && pwd); else
  bins="$out/build-bin"; mkdir "$bins"
  (cd "$repo/backend"
    # The Fleet Agent's JSON dependency relies on the Go version in go.mod.
    go_version=$(awk '$1 == "go" { print $2; exit }' go.mod)
    export GOTOOLCHAIN="${GOTOOLCHAIN:-go$go_version}"
    for entry in 'backend:.' 'stream-executor:./cmd/stream-executor' 'source-modbus:./cmd/source-modbus' 'fleet-agent:./fleet-agent/cmd/fleet-agent' 'native-launcher:./cmd/native-launcher'; do
      name=${entry%%:*}; source=${entry#*:}
      CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -tags ts_omit_useproxy -ldflags "-s -w -X backend/internal/runtimeenv.BuildDeployment=native -X backend/internal/runtimeenv.BuildVersion=$version -X backend/internal/runtimeenv.BuildRevision=$revision" -o "$bins/$name" "$source"
    done)
fi
for name in backend stream-executor source-modbus fleet-agent; do cp "$bins/$name" "$payload/backend/"; done
cp "$bins/native-launcher" "$payload/"
chmod 755 "$payload/native-launcher" "$payload/backend/"{backend,stream-executor,source-modbus,fleet-agent}
cp -R "$repo/backend/etc" "$payload/backend/etc"
cp "$repo/backend/etc/backend.yaml" "$payload/backend/backend.template.yaml"
cp -R "$repo/backend/http" "$payload/backend/http"
cp -R "$web" "$payload/web"
cp "$cache/node-v22.23.1-linux-x64/bin/node" "$payload/node/node"
npm install --prefix "$payload/node" --ignore-scripts --omit=dev --no-audit --no-fund pnpm@10.13.1
cp -RL "$cache/node-v22.23.1-linux-x64/lib/node_modules/npm" "$payload/node/node_modules/"
for cli in npm pnpm; do
  if [[ $cli == npm ]]; then entry='npm/bin/npm-cli.js'; else entry='pnpm/bin/pnpm.cjs'; fi
  printf '#!/bin/sh\nbase=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)\nexec "$base/node" "$base/node_modules/%s" "$@"\n' "$entry" > "$payload/node/$cli"
  chmod 755 "$payload/node/$cli"
done
find "$payload/node/node_modules/pnpm/dist" -type f \( -name 'reflink.darwin-*' -o -name 'reflink.win32-*' -o -name 'fastlist-*.exe' \) -delete
for pair in source-opcua:opcua mqtt-native:mqtt; do
  source=${pair%%:*}; target=${pair#*:}; mkdir "$payload/$target"
  cp "$repo/runtime/$source/"*.mjs "$repo/runtime/$source/"*.json "$payload/$target/"
  (cd "$payload/$target"; npm ci --omit=dev --no-audit --no-fund)
done
cp "$cache/nats-server-v2.15.2-linux-amd64/nats-server" "$payload/nats/"
cp -RL "$cache/postgres" "$payload/postgres"
# Dereference only dependencies assembled in the new payload, never customer data.
find "$payload" -type l | while IFS= read -r link; do
  resolved=$(realpath "$link")
  [[ $resolved == "$payload/"* ]] || { echo "Escaping dependency link: $link" >&2; exit 1; }
  cp --remove-destination "$resolved" "$link"
done
cp "$cache/licenses/"* "$payload/licenses/"
cp "$cache/node-v22.23.1-linux-x64/LICENSE" "$payload/licenses/Node-LICENSE.txt"
cp "$cache/nats-server-v2.15.2-linux-amd64/LICENSE" "$payload/licenses/NATS-LICENSE.txt"
cp "$repo/scripts/deploy/linux-native-dependencies.json" "$payload/licenses/dependency-sources.json"
cp "$repo/deploy/linux-native/"* "$payload/"
chmod 755 "$payload/install.sh" "$payload/tier0"
node "$repo/scripts/deploy/seal-linux-native.mjs" "$payload"
node "$payload/verify-package.mjs" "$payload"
buildVersion=$(node -p 'require(process.argv[1]).packageVersion' "$payload/package.json")
archive="Tier0-Edge-$buildVersion-linux-amd64.tar.gz"
tar -czf "$out/$archive" -C "$out" payload
(cd "$out"; sha256sum "$archive" > SHA256SUMS)
echo "Ready: $out/$archive"
