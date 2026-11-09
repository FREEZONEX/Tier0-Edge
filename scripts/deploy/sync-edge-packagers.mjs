#!/usr/bin/env node
// Project the reviewed upstream packaging scripts into the Edge source tree.
import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const [upstream, ref, mode = '--check', ...extra] = process.argv.slice(2);
if (!upstream || !ref || extra.length || !['--check', '--write'].includes(mode)) {
  throw Error('Usage: node scripts/deploy/sync-edge-packagers.mjs UPSTREAM_CHECKOUT REF [--check|--write]');
}
const directory = path.dirname(fileURLToPath(import.meta.url));
const git = (repo, args) => execFileSync('git', ['-C', repo, ...args], { encoding: 'utf8' });
const revision = git(upstream, ['rev-parse', '--verify', '--end-of-options', ref + '^{commit}']).trim();
if (mode === '--write' && git(directory, ['status', '--porcelain']).trim()) {
  throw Error('Sync requires a clean Edge checkout');
}

function cut(source, anchor, replacement) {
  if (!anchor.test(source)) throw Error('Upstream packaging rule changed: ' + anchor);
  return source.replace(anchor, replacement);
}

const licenseManifest = "Get-Content -LiteralPath (Join-Path $PSScriptRoot 'windows-native-licenses.json') -Raw | ConvertFrom-Json -AsHashtable";
const projection = {
  'assert-windows-native-payload.ps1': (s) => s,
  'product-package.mjs': (s) => s,
  'test-windows-native-defender.ps1': (s) => s,
  'package.mjs': (s) => cut(s, /--edition edge\|enterprise/, '--edition edge'),
  'package-linux-native.sh': (s) => s,
  'package-macos-native.sh': (s) => s,
  'macos-native-app.swift': (s) => s,
  'prepare-macos-cross-cache.sh': (s) => s,
  'seal-macos-native.mjs': (s) => s,
  'verify-macos-cross-dmg.sh': (s) => s,
  'macos-dmg/applications_alias.go': (s) => s,
  'macos-dmg/go.mod': (s) => s,
  'macos-dmg/go.sum': (s) => s,
  'macos-dmg/main.go': (s) => s,
  'package-windows-native.ps1': (s) => {
    s = cut(s, /\[ValidateSet\('enterprise','edge'\)\]\[string\]\$Edition = 'enterprise'/, "[ValidateSet('edge')][string]$Edition = 'edge'");
    s = cut(s, /^Copy-Item [^\n]*'deploy\/windows-native\/VALIDATION\.md'[^\n]*\n/m, '');
    return cut(s, /^Copy-Tree \(Join-Path \$CacheRoot 'licenses'\) \(Join-Path \$OutputRoot 'licenses'\)$/m, [
      '$licenses = ' + licenseManifest,
      "$licenseTarget = Join-Path $OutputRoot 'licenses'",
      'New-Item -ItemType Directory -Force -Path $licenseTarget | Out-Null',
      'foreach ($name in $licenses.Keys) {',
      '  Copy-Item -LiteralPath (Join-Path $CacheRoot "licenses/$name") -Destination $licenseTarget',
      '}',
    ].join('\n'));
  },
  'seal-windows-native.ps1': (s) => cut(s,
    /if \(\$manifest\.productEdition -notin @\('edge','enterprise'\)\) \{ throw 'Payload productEdition must be edge or enterprise\.' \}/,
    "if ($manifest.productEdition -ne 'edge') { throw 'Payload productEdition must be edge.' }"),
  'test-windows-native-uninstall.ps1': (s) => s,
  'fetch-windows-native.ps1': (s) => cut(s, /^\$licenses = @\{[\s\S]*?^\}$/m, '$licenses = ' + licenseManifest),
};

const updates = [];
for (const [name, project] of Object.entries(projection)) {
  const upstreamFile = git(upstream, ['show', revision + ':scripts/deploy/' + name]).replaceAll('\r\n', '\n');
  const source = project(upstreamFile).replace(/-X backend\/internal\/runtimeenv\.BuildEdition=[^\s]+[ \t]+/g, '');
  if (source.includes('runtimeenv.BuildEdition')) throw Error('Unrecognized edition override in ' + name);
  const target = path.join(directory, name);
  if (!existsSync(target) || readFileSync(target, 'utf8').replaceAll('\r\n', '\n') !== source) {
    const mode = git(upstream, ['ls-tree', revision, '--', 'scripts/deploy/' + name]).startsWith('100755') ? 0o755 : 0o644;
    updates.push({ target, source, name, mode });
  }
}
for (const { target, source, name, mode: fileMode } of updates) {
  console.log((mode === '--write' ? 'Updated ' : 'Differs ') + name);
  if (mode === '--write') {
    mkdirSync(path.dirname(target), { recursive: true });
    writeFileSync(target, source, { mode: fileMode });
  }
}
if (mode === '--check' && updates.length) process.exitCode = 1;
if (!updates.length) console.log('Edge packagers match the reviewed projection of ' + revision);
