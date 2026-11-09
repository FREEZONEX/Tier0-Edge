#!/usr/bin/env bash
set -euo pipefail
if [[ ${1:-} == --help || $# -lt 3 ]]; then
  echo 'Usage: package-macos-native.sh CACHE NEW_OUTPUT WEB_DIST [PREBUILT_BIN]'
  echo 'Build on Apple Silicon or Linux x64. On Linux use prepare-macos-cross-cache.sh first.'
  exit 0
fi
host="$(uname -s)/$(uname -m)"
[[ $host == Darwin/arm64 || $host == Linux/x86_64 ]] || { echo 'Build on Apple Silicon or Linux x64' >&2; exit 1; }
repo=$(cd "$(dirname "$0")/../.." && pwd)
cache=$(cd "$1" && pwd)
out=$2
web=$(cd "$3" && pwd)
[[ ! -e "$out" && ! -L "$out" ]] || { echo 'Output must be new' >&2; exit 1; }
swiftc=(swiftc)
clang=(clang)
if [[ $host == Linux/x86_64 ]]; then
  [[ $# == 3 ]] || { echo 'Linux cross-build compiles its Go binaries from the current checkout' >&2; exit 1; }
  if [[ -n ${TIER0_MACOS_SDK:-} ]]; then
    sdk=$TIER0_MACOS_SDK
    swift_root=${TIER0_MACOS_SWIFT_ROOT:?Set TIER0_MACOS_SWIFT_ROOT to the Linux Swift toolchain usr directory}
    swift_resource=${TIER0_MACOS_SWIFT_RESOURCE:?Set TIER0_MACOS_SWIFT_RESOURCE to the Darwin Swift resource directory}
    linker=${TIER0_MACOS_LD64:-/usr/lib/llvm-19/bin/ld64.lld}
    shims="$sdk/usr/lib/swift/shims"
    [[ -f "$shims/module.modulemap" && -d "$swift_resource/macosx" && -x "$linker" ]] ||
      { echo 'Incomplete macOS SDK, Darwin Swift resources, or ld64 toolchain' >&2; exit 1; }
    swiftc=("$swift_root/bin/swiftc" -sdk "$sdk" -resource-dir "$swift_resource" -I "$shims"
      -Xcc "-fmodule-map-file=$shims/module.modulemap" -Xclang-linker "--ld-path=$linker" -Xclang-linker -mlinker-version=1115.7.3)
    if [[ -n ${TIER0_MACOS_SWIFT_MODULE_CACHE:-} ]]; then
      swiftc+=(-module-cache-path "$TIER0_MACOS_SWIFT_MODULE_CACHE")
    fi
    clang=("$swift_root/bin/clang" -isysroot "$sdk" "--ld-path=$linker" -mlinker-version=1115.7.3)
  else
    swiftc=("${TIER0_MACOS_SWIFTC:-arm64-apple-macos-swiftc}")
    clang=("${TIER0_MACOS_CLANG:-arm64-apple-macos-clang}")
  fi
  for tool in "${swiftc[0]}" "${clang[0]}" rcodesign; do
    command -v "$tool" >/dev/null || { echo "Missing macOS cross-build tool: $tool" >&2; exit 1; }
  done
  (cd "$cache"; sha256sum -c --quiet SHA256SUMS)
else
  export PATH="$cache/node-v22.23.1-darwin-arm64/bin:$PATH"
fi
mkdir -p "$out"
out=$(cd "$out" && pwd)
plan=$(node "$repo/scripts/deploy/product-package.mjs" plan edge darwin/arm64 native)
version=$(printf '%s' "$plan" | node -e 'let b="";process.stdin.on("data",d=>b+=d).on("end",()=>console.log(JSON.parse(b).productVersion))')
revision=$(git -C "$repo" rev-parse HEAD)
package_version=$(node "$repo/scripts/deploy/product-package.mjs" build-version)
image="$out/image"
app="$image/Tier0 Edge.app"
payload="$app/Contents/Resources/runtime"
mkdir -p "$payload/backend" "$app/Contents/MacOS" "$payload/licenses" "$payload/node/node_modules"
if [[ $# -gt 3 ]]; then
  bins=$(cd "$4" && pwd)
else
  bins="$out/build-bin"
  mkdir "$bins"
  (cd "$repo/backend"
    for entry in 'backend:.' 'stream-executor:./cmd/stream-executor' 'source-modbus:./cmd/source-modbus' 'fleet-agent:./fleet-agent/cmd/fleet-agent' 'native-launcher:./cmd/native-launcher'; do
      name=${entry%%:*}; source=${entry#*:}
      CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -tags ts_omit_useproxy -ldflags "-s -w -X backend/internal/runtimeenv.BuildDeployment=native -X backend/internal/runtimeenv.BuildVersion=$version -X backend/internal/runtimeenv.BuildRevision=$revision" -o "$bins/$name" "$source"
    done)
fi
for name in backend stream-executor source-modbus fleet-agent; do cp "$bins/$name" "$payload/backend/"; done
"${clang[@]}" -O2 -Wall -Werror -target arm64-apple-macosx13.0 "$repo/runtime/usb-hid-native/reader.c" \
  -framework IOKit -framework CoreFoundation -o "$payload/backend/tier0-hid-reader"
cp "$bins/native-launcher" "$payload/"
chmod 755 "$payload/native-launcher" "$payload/backend/"{backend,stream-executor,source-modbus,fleet-agent}
cp -R "$repo/backend/etc" "$payload/backend/etc"
cp "$repo/backend/etc/backend.yaml" "$payload/backend/backend.template.yaml"
cp -R "$repo/backend/http" "$payload/backend/http"
cp -R "$web" "$payload/web"
cp "$repo/frontend/apps/web/public/favicon.ico" "$payload/favicon.ico"
cp "$payload/favicon.ico" "$app/Contents/Resources/"
cp "$cache/node-v22.23.1-darwin-arm64/bin/node" "$payload/node/node"
cp "$cache/node-v22.23.1-darwin-arm64/LICENSE" "$payload/licenses/Node-LICENSE.txt"
npm install --prefix "$payload/node" --ignore-scripts --omit=dev --no-audit --no-fund pnpm@10.13.1
cp -R "$cache/node-v22.23.1-darwin-arm64/lib/node_modules/npm" "$payload/node/node_modules/"
find "$payload/node/node_modules/pnpm/dist" -type f \( -name 'reflink.darwin-x64-*' -o -name 'reflink.win32-*' -o -name 'fastlist-*.exe' \) -delete
for cli in npm pnpm; do
  if [[ $cli == npm ]]; then entry='npm/bin/npm-cli.js'; else entry='pnpm/bin/pnpm.cjs'; fi
  printf '#!/bin/sh\nbase=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)\nexec "$base/node" "$base/node_modules/%s" "$@"\n' "$entry" > "$payload/node/$cli"
  chmod 755 "$payload/node/$cli"
done
# JS CLIs stay portable; native helper packages for other platforms are omitted by npm.
for pair in source-opcua:opcua mqtt-native:mqtt; do
  source=${pair%%:*}; target=${pair#*:}
  mkdir "$payload/$target"
  cp "$repo/runtime/$source/"*.mjs "$repo/runtime/$source/"*.json "$payload/$target/"
  (cd "$payload/$target"; npm ci --omit=dev --no-audit --no-fund --os=darwin --cpu=arm64)
done
mkdir "$payload/nats"
cp "$cache/nats-server-v2.15.2-darwin-arm64/nats-server" "$payload/nats/"
cp "$cache/nats-server-v2.15.2-darwin-arm64/LICENSE" "$payload/licenses/NATS-LICENSE.txt"
if [[ $host == Linux/x86_64 ]]; then
  cp -R "$cache/postgres" "$payload/postgres"
  cp "$cache/licenses/"* "$payload/licenses/"
else
  node "$repo/scripts/deploy/prepare-macos-postgres.mjs" "$cache/postgres" "$payload/postgres"
  cp "$cache/timescaledb-2.20.0/LICENSE" "$payload/licenses/TimescaleDB-LICENSE.txt"
  cp "$cache/timescaledb-2.20.0/tsl/LICENSE-TIMESCALE" "$payload/licenses/TimescaleDB-TSL-LICENSE.txt"
  cp "$cache/PostgresApp-Credits.rtf" "$payload/licenses/"
fi
cp "$repo/scripts/deploy/macos-native-dependencies.json" "$payload/licenses/dependency-sources.json"
if [[ -f "$cache/postgres/share/doc/postgresql/COPYRIGHT" ]]; then cp "$cache/postgres/share/doc/postgresql/COPYRIGHT" "$payload/licenses/PostgreSQL-COPYRIGHT.txt"; fi
"${swiftc[@]}" -O -target arm64-apple-macosx13.0 "$repo/scripts/deploy/macos-native-app.swift" -o "$app/Contents/MacOS/Tier0Edge"
mkdir -p "$image/.background"
if [[ $host == Linux/x86_64 ]]; then
  cmp "$cache/artwork/favicon.ico" "$payload/favicon.ico"
  cp "$cache/artwork/Tier0Edge.icns" "$app/Contents/Resources/Tier0Edge.icns"
  cp "$cache/artwork/background.png" "$image/.background/background.png"
else
  swiftc -O "$repo/scripts/deploy/macos-dmg-artwork.swift" -o "$out/dmg-artwork"
  "$out/dmg-artwork" "$out/artwork" "$repo/frontend/apps/web/public/favicon.ico"
  iconutil -c icns "$out/artwork/Tier0Edge.iconset" -o "$app/Contents/Resources/Tier0Edge.icns"
  cp "$out/artwork/background.png" "$image/.background/background.png"
  swift "$repo/scripts/deploy/macos-applications-alias.swift" "$image/Applications"
fi
cat > "$app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>CFBundleIdentifier</key><string>cn.tier0.edge.native</string><key>CFBundleName</key><string>Tier0 Edge</string><key>CFBundleExecutable</key><string>Tier0Edge</string><key>CFBundleVersion</key><string>${version%%-r*}</string><key>CFBundleShortVersionString</key><string>${version%%-r*}</string><key>Tier0PackageVersion</key><string>$package_version</string><key>LSMinimumSystemVersion</key><string>13.0</string><key>LSUIElement</key><true/><key>CFBundleIconFile</key><string>Tier0Edge.icns</string></dict></plist>
PLIST
cp "$repo/deploy/macos-native/README.txt" "$image/安装说明.txt"
product=$(sed -n 's/^PRODUCT_VERSION=//p' "$repo/deploy/.env.default" | tr -d '\r')
SOURCE_REVISION=${SOURCE_REVISION:-$(git -C "$repo" rev-parse HEAD)} PACKAGE_VERSION="$package_version" PRODUCT_VERSION="$product" node "$repo/scripts/deploy/seal-macos-native.mjs" "$payload" "$app"
dmg="Tier0-Edge-$package_version-macOS-arm64.dmg"
if [[ $host == Linux/x86_64 ]]; then
  (cd "$repo/scripts/deploy/macos-dmg"; go run . "$image" "$out/$dmg" "Tier0 Edge $package_version" "$cache/artwork")
  (cd "$out"; sha256sum "$dmg" > SHA256SUMS)
else
  bash "$repo/scripts/deploy/create-macos-dmg.sh" "$image" "$out/$dmg" "Tier0 Edge $package_version" "$cache"
  (cd "$out"; shasum -a 256 "$dmg" > SHA256SUMS)
fi
echo "Ready: $out/$dmg"
