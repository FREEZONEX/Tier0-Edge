#!/usr/bin/env bash
set -euo pipefail
if [[ ${1:-} == --help ]]; then
  echo 'install.sh [--install-dir NEW_DIR] [--data-dir DIR] [--port-offset 0..40000] [--no-start] [--desktop]'
  echo 'Run as a non-root user. Upgrade: choose NEW program directory and existing data directory; stop old instance first.'
  exit 0
fi
[[ $(uname -s) == Linux && $(uname -m) == x86_64 && $EUID != 0 ]] || { echo 'Linux AMD64, non-root user required' >&2; exit 1; }
umask 077
package=$(cd -- "$(dirname -- "$0")" && pwd -P)
node="$package/node/node"
version=$("$node" -p 'require(process.argv[1]).packageVersion' "$package/package.json")
target="$HOME/.local/opt/tier0-edge/$version"
data="$HOME/.local/share/tier0-edge"
offset=-1
start=yes
desktop=no
while (($#)); do
  case "$1" in
    --install-dir|--data-dir|--port-offset)
      (($#>=2)) || { echo "Missing value: $1" >&2; exit 2; }
      case "$1" in --install-dir) target=$2;; --data-dir) data=$2;; --port-offset) offset=$2;; esac
      shift 2;;
    --no-start) start=no; shift;;
    --desktop) desktop=yes; shift;;
    *) echo "Unknown option: $1" >&2; exit 2;;
  esac
done
if [[ $offset != -1 ]]; then
  [[ $offset =~ ^0*([0-9]{1,5})$ ]] || { echo 'Invalid port offset' >&2; exit 2; }
  offset=$((10#${BASH_REMATCH[1]}))
  ((offset <= 40000)) || { echo 'Invalid port offset' >&2; exit 2; }
fi
target=$(realpath -m -- "$target")
data=$(realpath -m -- "$data")
if [[ $target == / || $data == / || $target == "$data" || $data == "$target/"* || $target == "$data/"* ||
      $target == "$package" || $target == "$package/"* || $package == "$target/"* ||
      $data == "$package" || $data == "$package/"* || $package == "$data/"* ]]; then
  echo 'Program, package and data directories must be separate' >&2
  exit 1
fi
[[ ! -e $target && ! -L $target ]] || { echo 'Install directory must be new; keep old program for recovery' >&2; exit 1; }
if [[ -f $data/state/supervisor.lock ]]; then
  flock -n "$data/state/supervisor.lock" true || { echo 'Stop the existing installation before upgrade' >&2; exit 1; }
fi
"$package/native-launcher" --product-identity
echo 'Checking package checksums and target architecture...'
"$node" "$package/verify-package.mjs" "$package"
mkdir -p -- "$(dirname -- "$target")"
# Atomic directory claim prevents concurrent installers overwriting one another.
mkdir -- "$target"
cp -a -- "$package/." "$target/"
"$node" --input-type=module - "$target" "$data" "$offset" <<'JS'
import fs from 'node:fs';
fs.writeFileSync(process.argv[2]+'/.installation.json',JSON.stringify({dataRoot:process.argv[3],portOffset:Number(process.argv[4])})+'\n',{mode:0o600,flag:'wx'});
JS
if [[ $desktop == yes ]]; then
  mkdir -p "$HOME/.local/share/applications"
  "$node" --input-type=module - "$target" "$HOME/.local/share/applications" <<'JS'
import fs from 'node:fs';import crypto from 'node:crypto';
const root=process.argv[2], id=crypto.createHash('sha256').update(root).digest('hex').slice(0,12);
if(/[\r\n]/.test(root))throw Error('Desktop entry requires a single-line path');
const quote=s=>'"'+s.replace(/[\\"`$]/g,'\\$&').replace(/%/g,'%%')+'"';
fs.writeFileSync(process.argv[3]+'/tier0-edge-'+id+'.desktop','[Desktop Entry]\nType=Application\nName=Tier0 Edge\nTerminal=true\nExec='+quote(root+'/tier0')+' start\n',{mode:0o600});
JS
fi
printf 'Installed program: %s\nPersistent data: %s\nControl: %s/tier0 start|stop|status|logs\n' "$target" "$data" "$target"
if [[ $start == yes ]]; then exec "$target/tier0" start; fi
echo 'Prepared only: data initialization/upgrade is pending until a successful start.'
