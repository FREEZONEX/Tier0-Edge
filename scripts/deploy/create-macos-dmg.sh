#!/usr/bin/env bash
set -euo pipefail
[[ $# == 4 ]] || { echo 'Usage: create-macos-dmg.sh IMAGE OUTPUT_DMG VOLUME_NAME BUILD_CACHE'; exit 1; }
repo=$(cd "$(dirname "$0")/../.." && pwd)
source_dir=$(cd "$1" && pwd)
destination=$2
volume=$3
cache=$4
[[ ! -e "$destination" ]] || { echo 'DMG output already exists'; exit 1; }
# Isolated build-only dependencies; nothing is installed on an end-user Mac.
python_env="$cache/dmg-layout-python"
if [[ ! -x "$python_env/bin/python3" ]]; then python3 -m venv "$python_env"; fi
"$python_env/bin/python3" -m pip install --disable-pip-version-check 'ds-store==1.3.1' 'mac-alias==2.2.2'
work=$(mktemp -d "${TMPDIR:-/tmp}/tier0-dmg.XXXXXX")
mounted=false
cleanup() {
  if $mounted; then hdiutil detach "$work/mount" >/dev/null || true; fi
  # Retain the scratch image on failure for diagnostics.
}
trap cleanup EXIT
hdiutil create -volname "$volume" -srcfolder "$source_dir" -format UDRW "$work/layout.dmg" >/dev/null
mkdir "$work/mount"
hdiutil attach -nobrowse -noautoopen -mountpoint "$work/mount" "$work/layout.dmg" >/dev/null
mounted=true
"$python_env/bin/python3" "$repo/scripts/deploy/macos-dmg-layout.py" "$work/mount"
# Finder reads the volume's saved view when opened. Do not use bless
# --openfolder: Apple Silicon explicitly does not support that operation.
sync
hdiutil detach "$work/mount" >/dev/null
mounted=false
hdiutil convert "$work/layout.dmg" -format UDZO -o "$destination" >/dev/null
# Check the final compressed image after relocation, not only staging metadata.
hdiutil attach -readonly -nobrowse -noautoopen -mountpoint "$work/mount" "$destination" >/dev/null
mounted=true
"$python_env/bin/python3" "$repo/scripts/deploy/macos-dmg-layout.py" "$work/mount" --verify
hdiutil detach "$work/mount" >/dev/null
mounted=false
echo "Created styled DMG: $destination (scratch: $work)"
