param(
  [Parameter(Mandatory)][string]$CacheRoot,
  [Parameter(Mandatory)][string]$OutputRoot,
  [string]$PNPMRoot,
  [string]$WebRoot,
  [ValidateSet('edge')][string]$Edition = 'edge'
)
$ErrorActionPreference = 'Stop'
$gcc = 'gcc'
if ($IsLinux) {
  $env:GOOS = 'windows'
  $env:GOARCH = 'amd64'
  $env:CGO_ENABLED = '0'
  $gcc = 'x86_64-w64-mingw32-gcc'
  $wine = $env:TIER0_WINDOWS_WINE
  if (!$wine) { $wine = 'wine' }
  if (!(Get-Command $wine -ErrorAction SilentlyContinue)) { throw "Cannot find $wine. Install Wine x64 for binary identity checks or set TIER0_WINDOWS_WINE." }
}
$goTarget = (& go env -json GOOS GOARCH | ConvertFrom-Json)
if ($LASTEXITCODE -ne 0 -or $goTarget.GOOS -ne 'windows' -or $goTarget.GOARCH -ne 'amd64') { throw 'Native Windows packaging requires the windows/amd64 Go target.' }
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
$productPlan = (& node (Join-Path $PSScriptRoot 'product-package.mjs') plan $Edition windows/amd64 native | ConvertFrom-Json)
if ($LASTEXITCODE -ne 0) { throw 'Unsupported product package selection.' }
$sourceRevision = (& git -C $repoRoot rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0) { throw 'Cannot identify source revision.' }
$CacheRoot = (Resolve-Path $CacheRoot).Path
$pnpmVersion = ((Get-Content -LiteralPath (Join-Path $repoRoot 'frontend/package.json') -Raw | ConvertFrom-Json).packageManager -split '@')[-1]
if ($IsLinux -and !$PNPMRoot) { throw 'Specify -PNPMRoot (or --pnpm) with the extracted pnpm package on Linux.' }
if (!$PNPMRoot) { $PNPMRoot = Join-Path $env:LOCALAPPDATA "node/corepack/v1/pnpm/$pnpmVersion" }
$PNPMRoot = (Resolve-Path -LiteralPath $PNPMRoot).Path
if (!$WebRoot) { $WebRoot = Join-Path $repoRoot 'frontend/apps/web/dist' }
if ((Get-Content -LiteralPath (Join-Path $PNPMRoot 'package.json') -Raw | ConvertFrom-Json).version -ne $pnpmVersion) { throw 'Bundled pnpm must match the frontend packageManager version.' }
$dependencies = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'windows-native-dependencies.json') -Raw | ConvertFrom-Json
foreach ($dependency in $dependencies) {
  if ((Get-FileHash -LiteralPath (Join-Path $CacheRoot $dependency.name)).Hash -ne $dependency.sha256) { throw "Dependency checksum mismatch: $($dependency.name)" }
}
$OutputRoot = [IO.Path]::GetFullPath($OutputRoot)
if (Test-Path -LiteralPath $OutputRoot) { throw 'OutputRoot must be a new directory, never an existing installation.' }
New-Item -ItemType Directory -Path $OutputRoot | Out-Null
function Copy-Tree([string]$Source,[string]$Target,[switch]$Pnpm) {
  if (!(Test-Path -LiteralPath $Source)) { throw "Missing package input: $Source" }
  New-Item -ItemType Directory -Force -Path $Target | Out-Null
  $excludedFiles = @('.corepack','reflink.darwin-*','reflink.win32-arm64-*','fastlist-*-x86.exe')
  $excludedDirs = @('.git','test','tests','__tests__')
  if ($IsLinux) {
    & cp -R -- (Join-Path $Source '.') $Target
    if ($LASTEXITCODE -ne 0) { throw "Copy failed: $Source" }
    if ($Pnpm) {
      foreach ($pattern in $excludedFiles) {
        Get-ChildItem -LiteralPath $Target -File -Force -Recurse -Filter $pattern | Remove-Item -Force
      }
      foreach ($name in $excludedDirs) {
        Get-ChildItem -LiteralPath $Target -Directory -Force -Recurse -Filter $name | Sort-Object { $_.FullName.Length } -Descending | Remove-Item -Force -Recurse
      }
    }
  } else {
    $copyArgs = @($Source,$Target,'/E','/NFL','/NDL','/NJH','/NJS','/NP','/R:1','/W:1','/XJ')
    if ($Pnpm) { $copyArgs += @('/XF') + $excludedFiles + @('/XD') + $excludedDirs }
    & robocopy @copyArgs
    if ($LASTEXITCODE -ge 8) { throw "Copy failed: $Source" }
  }
}
if (!(Get-Command $gcc -ErrorAction SilentlyContinue)) { throw "Packaging USB HID input requires $gcc (MinGW-w64)." }
Push-Location (Join-Path $repoRoot 'backend')
try {
  foreach ($component in @(@('backend','.'),@('stream-executor','./cmd/stream-executor'),@('source-modbus','./cmd/source-modbus'),@('fleet-agent','./fleet-agent/cmd/fleet-agent'),@('Start','./cmd/native-launcher'),@('Uninstall','./cmd/native-uninstaller'))) {
    $linkFlags = "-s -w -X backend/internal/runtimeenv.BuildDeployment=native -X backend/internal/runtimeenv.BuildVersion=$($productPlan.productVersion) -X backend/internal/runtimeenv.BuildRevision=$sourceRevision"
    if ($component[0] -in @('Start','Uninstall')) { $linkFlags += ' -H=windowsgui' }
    $buildArgs = @('build','-buildvcs=false','-trimpath','-ldflags',$linkFlags)
    # Native networking follows Backend's direct center connection policy.
    # Do not silently inherit desktop WinHTTP proxy / integrated proxy auth.
    if ($component[0] -eq 'fleet-agent') { $buildArgs += @('-tags','ts_omit_useproxy') }
    $buildArgs += @('-o',(Join-Path $OutputRoot ($component[0]+'.exe')),$component[1])
    & go @buildArgs
    if ($LASTEXITCODE -ne 0) { throw "Build failed: $($component[0])" }
  }
  $tailscaleModule = (& go list -m -json tailscale.com | ConvertFrom-Json)
  if ($LASTEXITCODE -ne 0) { throw 'Cannot resolve embedded Tailscale module' }
} finally { Pop-Location }
New-Item -ItemType Directory -Force -Path (Join-Path $OutputRoot 'backend') | Out-Null
& $gcc -O2 -Wall -Werror -municode (Join-Path $repoRoot 'runtime/usb-hid-native/reader_windows.c') -o (Join-Path $OutputRoot 'backend/tier0-hid-reader.exe') -luser32 -lcfgmgr32 -lhid
if ($LASTEXITCODE -ne 0) { throw 'USB HID reader compilation failed.' }
foreach ($name in @('backend','stream-executor','source-modbus','fleet-agent')) {
  Move-Item -LiteralPath (Join-Path $OutputRoot "$name.exe") -Destination (Join-Path $OutputRoot 'backend')
}
Copy-Tree (Join-Path $repoRoot 'backend/etc') (Join-Path $OutputRoot 'backend/etc')
Copy-Item -LiteralPath (Join-Path $repoRoot 'backend/etc/backend.yaml') -Destination (Join-Path $OutputRoot 'backend/backend.template.yaml')
Copy-Tree (Join-Path $repoRoot 'backend/http') (Join-Path $OutputRoot 'backend/http')
Copy-Tree $WebRoot (Join-Path $OutputRoot 'web')
foreach ($dir in @('bin','lib','share')) {Copy-Tree (Join-Path $CacheRoot "postgresql/pgsql/$dir") (Join-Path $OutputRoot "postgres/$dir")}
Get-ChildItem -LiteralPath (Join-Path $CacheRoot 'timescale/timescaledb') -Filter '*.dll' | Copy-Item -Destination (Join-Path $OutputRoot 'postgres/lib')
Get-ChildItem -LiteralPath (Join-Path $CacheRoot 'timescale/timescaledb') -Include '*.sql','*.control' -Recurse | Copy-Item -Destination (Join-Path $OutputRoot 'postgres/share/extension')
foreach ($target in @('postgres/bin','node')) {
  New-Item -ItemType Directory -Force -Path (Join-Path $OutputRoot $target) | Out-Null
  Get-ChildItem -LiteralPath (Join-Path $CacheRoot 'vccrt') -Filter '*.dll_amd64' | ForEach-Object {
    Copy-Item -LiteralPath $_.FullName -Destination (Join-Path $OutputRoot "$target/$($_.Name.Replace('_amd64',''))")
  }
}
New-Item -ItemType Directory -Force -Path (Join-Path $OutputRoot 'nats') | Out-Null
Get-ChildItem -LiteralPath (Join-Path $CacheRoot 'nats') -Filter '*.exe' -Recurse | Copy-Item -Destination (Join-Path $OutputRoot 'nats')
New-Item -ItemType Directory -Force -Path (Join-Path $OutputRoot 'node') | Out-Null
foreach ($file in @('node.exe','LICENSE')) { Copy-Item -LiteralPath (Join-Path $CacheRoot "node/node-v22.23.1-win-x64/$file") -Destination (Join-Path $OutputRoot 'node') }
Copy-Tree (Join-Path $CacheRoot 'node/node-v22.23.1-win-x64/node_modules/npm') (Join-Path $OutputRoot 'node/node_modules/npm')
# pnpm ships optional native helpers for other platforms. Copy only the x64 Windows variant.
Copy-Tree $PNPMRoot (Join-Path $OutputRoot 'node/node_modules/pnpm') -Pnpm
Copy-Item -LiteralPath (Join-Path $repoRoot 'frontend/apps/web/public/favicon.ico') -Destination (Join-Path $OutputRoot 'tier0.ico')
foreach ($pair in @(@('source-opcua','opcua'),@('mqtt-native','mqtt'))) {
  $source = Join-Path $repoRoot "runtime/$($pair[0])"
  $target = Join-Path $OutputRoot $pair[1]
  New-Item -ItemType Directory -Force -Path $target | Out-Null
  Get-ChildItem -LiteralPath $source -File | Where-Object { $_.Extension -in @('.mjs','.json') } | Copy-Item -Destination $target
  Copy-Tree (Join-Path $source 'node_modules') (Join-Path $target 'node_modules')
}
$commit = & git -C $repoRoot rev-parse HEAD
if ($LASTEXITCODE -ne 0) { throw 'Cannot read source revision' }
$sourceDirty = [bool](& git -C $repoRoot status --porcelain)
if ($LASTEXITCODE -ne 0) { throw 'Cannot inspect source state' }
$archiveHashes = @($dependencies | Where-Object name -like '*.zip' | ForEach-Object { @{name=$_.name;sha256=$_.sha256} })
$manifest = @{ schemaVersion=2; kind='windows-native-validation'; platform='windows'; architecture='amd64'; sourceRevision=$commit; createdAt=[DateTime]::UtcNow.ToString('o'); vision=$false; dockerRequired=$false; dependencies=$dependencies; archives=$archiveHashes; nodePackageManagers=@{npm='bundled-with-node';pnpm=$pnpmVersion}; network='isolated-tsnet'; limitations=@('Not a production release','Only portable Node source App ZIPs are supported; Docker images are not executed','MQTT 3.1.1 only, sessions and retained messages are volatile','No OS service/autostart integration; network stops with installation','No RustFS, Redis, Anchor, AI worker, Notebook or Vision runtime') }
$manifest.sourceDirty = $sourceDirty
$manifest.productEdition = $Edition
$manifest.licenseActivationRequired = $Edition -ne 'edge'
[IO.File]::WriteAllText((Join-Path $OutputRoot 'package.json'),($manifest | ConvertTo-Json -Depth 8))
& node (Join-Path $PSScriptRoot 'product-package.mjs') manifest $Edition windows/amd64 native $OutputRoot
if ($LASTEXITCODE -ne 0) { throw 'Shared product manifest verification failed.' }
Copy-Item -LiteralPath (Join-Path $repoRoot 'deploy/windows-native/README.txt') -Destination $OutputRoot
Copy-Item -LiteralPath (Join-Path $repoRoot 'deploy/windows-native/Stop.cmd') -Destination $OutputRoot
$licenses = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'windows-native-licenses.json') -Raw | ConvertFrom-Json -AsHashtable
$licenseTarget = Join-Path $OutputRoot 'licenses'
New-Item -ItemType Directory -Force -Path $licenseTarget | Out-Null
foreach ($name in $licenses.Keys) {
  Copy-Item -LiteralPath (Join-Path $CacheRoot "licenses/$name") -Destination $licenseTarget
}
Copy-Item -LiteralPath (Join-Path $tailscaleModule.Dir 'LICENSE') -Destination (Join-Path $OutputRoot 'licenses/Tailscale-LICENSE.txt')
Copy-Item -LiteralPath (Join-Path $repoRoot 'doc/windows-native-network.md') -Destination (Join-Path $OutputRoot 'NETWORK.md')
& (Join-Path $PSScriptRoot 'assert-windows-native-payload.ps1') -PayloadRoot $OutputRoot
Write-Host "Native payload ready: $OutputRoot"
# robocopy uses 1..7 for successful copies. Do not leak that status to wrappers.
$global:LASTEXITCODE = 0
