param([Parameter(Mandatory)][string]$PayloadRoot,[Parameter(Mandatory)][string]$OutputDirectory)
$ErrorActionPreference = 'Stop'
if ($IsLinux) {
  $env:GOOS = 'windows'
  $env:GOARCH = 'amd64'
  $env:CGO_ENABLED = '0'
}
$goTarget = (& go env -json GOOS GOARCH | ConvertFrom-Json)
if ($LASTEXITCODE -ne 0 -or $goTarget.GOOS -ne 'windows' -or $goTarget.GOARCH -ne 'amd64') { throw 'Native Windows installer sealing requires the windows/amd64 Go target.' }
$PayloadRoot = (Resolve-Path $PayloadRoot).Path
$defenderCheck = Join-Path $PSScriptRoot 'test-windows-native-defender.ps1'
foreach ($forbidden in @('data','state','logs','.installing','stop.request')) {
  if (Test-Path -LiteralPath (Join-Path $PayloadRoot $forbidden)) { throw "Runtime state must never enter installer: $forbidden" }
}
$manifest = Get-Content -LiteralPath (Join-Path $PayloadRoot 'package.json') -Raw | ConvertFrom-Json
if ($manifest.productEdition -ne 'edge') { throw 'Payload productEdition must be edge.' }
if ($manifest.packageVersion -cnotmatch ('^'+[regex]::Escape($manifest.productVersion)+'-[0-9a-f]{6}$')) { throw 'Missing or invalid packageVersion; rebuild using the current packager.' }
if (!$manifest.distribution -or $manifest.distribution.edition -ne $manifest.productEdition -or $manifest.distribution.platform -ne 'windows/amd64' -or $manifest.distribution.deployment -ne 'native') { throw 'Shared product manifest is missing or inconsistent.' }
if ($manifest.schemaVersion -ge 2 -and $manifest.sourceDirty) { throw 'Validation payload was built from uncommitted source. Commit and rebuild before sealing the deliverable.' }
if (!(Test-Path -LiteralPath (Join-Path $PayloadRoot 'Start.exe'))) { throw 'Start.exe is missing; refuse to publish an incomplete payload.' }
& (Join-Path $PSScriptRoot 'assert-windows-native-payload.ps1') -PayloadRoot $PayloadRoot
if ($IsWindows) { & $defenderCheck -ScanPath $PayloadRoot }
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
$buildDir = Join-Path $repoRoot ('backend/.build/native-installer-'+[Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $buildDir | Out-Null
$entries = @(Get-ChildItem -LiteralPath $PayloadRoot -File -Recurse | Where-Object Name -ne 'FILES.txt' | ForEach-Object {
  '{0}  {1}' -f $_.Length,[IO.Path]::GetRelativePath($PayloadRoot,$_.FullName).Replace('\','/')
})
[IO.File]::WriteAllLines((Join-Path $PayloadRoot 'FILES.txt'),$entries)
# The ZIP's CRC validates each extracted entry; the final EXE SHA-256 covers
# the complete embedded payload. Avoid reading thousands of files twice.
Write-Host "Compressing $($entries.Count) payload files..."
$sevenZip = (Get-Command 7z -ErrorAction Stop).Source
Push-Location $PayloadRoot
try {
  & $sevenZip a -tzip -mx=9 -mmt=on -y -bso0 -bsp0 (Join-Path $buildDir 'payload.zip') .
  if ($LASTEXITCODE -ne 0) { throw 'Payload compression failed' }
} finally { Pop-Location }
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'windows-native-installer.go') -Destination (Join-Path $buildDir 'main.go')
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'windows-native-upgrade.go') -Destination (Join-Path $buildDir 'upgrade.go')
$installer = Join-Path $OutputDirectory "Tier0-$($manifest.productEdition)-$($manifest.packageVersion)-Windows-x64-Setup.exe"
if (Test-Path -LiteralPath $installer) { throw 'Installer identity already exists; use a new build instead of overwriting it.' }
Push-Location $buildDir
try {
  Write-Host 'Building the self-contained installer...'
  & go build -buildvcs=false -trimpath -ldflags "-s -w -H=windowsgui -X backend/internal/runtimeenv.BuildDeployment=native -X backend/internal/runtimeenv.BuildVersion=$($manifest.productVersion) -X backend/internal/runtimeenv.BuildPackageVersion=$($manifest.packageVersion) -X backend/internal/runtimeenv.BuildRevision=$($manifest.sourceRevision)" -o $installer main.go upgrade.go
  if ($LASTEXITCODE -ne 0) { throw 'Installer compilation failed' }
} finally { Pop-Location }
if ($IsWindows) { & $defenderCheck -ScanPath $installer }
$hash = (Get-FileHash -LiteralPath $installer).Hash.ToLowerInvariant()
[IO.File]::WriteAllText(($installer+'.sha256'),($hash+'  '+[IO.Path]::GetFileName($installer)+[Environment]::NewLine))
if ($IsWindows) {
  $status = Get-MpComputerStatus -ErrorAction Stop
  $scanRecord = @{ scannedAt=[DateTime]::UtcNow.ToString('o'); sha256=$hash; payloadScan='passed'; installerScan='passed'; engineVersion=$status.AMEngineVersion; signatureVersion=$status.AntivirusSignatureVersion; realTimeProtectionEnabled=$status.RealTimeProtectionEnabled }
  [IO.File]::WriteAllText(($installer+'.defender.json'),($scanRecord | ConvertTo-Json))
} else {
  Write-Host 'Cross-built candidate: Defender scanning and installation validation are pending on Windows.'
}
Write-Host "Installer ready: $installer"
Write-Host "SHA-256: $hash"
