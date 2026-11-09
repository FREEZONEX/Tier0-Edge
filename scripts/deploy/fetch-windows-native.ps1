param([Parameter(Mandatory)][string]$CacheRoot)
$ErrorActionPreference = 'Stop'
$CacheRoot = [IO.Path]::GetFullPath($CacheRoot)
New-Item -ItemType Directory -Force -Path $CacheRoot | Out-Null
$dependencies = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'windows-native-dependencies.json') -Raw | ConvertFrom-Json
foreach ($dependency in $dependencies) {
  $archive = Join-Path $CacheRoot $dependency.name
  if (!(Test-Path -LiteralPath $archive)) {
    Invoke-WebRequest $dependency.url -OutFile ($archive+'.partial')
    if ((Get-FileHash -LiteralPath ($archive+'.partial')).Hash -ne $dependency.sha256) { throw "Download checksum mismatch: $($dependency.name)" }
    Move-Item -LiteralPath ($archive+'.partial') -Destination $archive
  }
  if ((Get-FileHash -LiteralPath $archive).Hash -ne $dependency.sha256) { throw "Cache checksum mismatch: $($dependency.name)" }
  if ($dependency.extract -and !(Test-Path -LiteralPath (Join-Path $CacheRoot $dependency.extract))) {
    Expand-Archive -LiteralPath $archive -DestinationPath (Join-Path $CacheRoot $dependency.extract)
  }
  Write-Host "Verified $($dependency.name) $($dependency.version)"
}
$licenseRoot = Join-Path $CacheRoot 'licenses'
New-Item -ItemType Directory -Force -Path $licenseRoot | Out-Null
$licenses = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'windows-native-licenses.json') -Raw | ConvertFrom-Json -AsHashtable
foreach ($license in $licenses.GetEnumerator()) {
  $path = Join-Path $licenseRoot $license.Key
  if (!(Test-Path -LiteralPath $path)) { Invoke-WebRequest $license.Value -OutFile $path }
}
# Extract Microsoft's redistributable app-local DLLs without installing an MSI,
# changing the registry, elevating, or requesting a reboot. 7z is a build tool only.
if (!(Test-Path -LiteralPath (Join-Path $CacheRoot 'vccrt/vcruntime140.dll_amd64'))) {
  $sevenZip = (Get-Command 7z -ErrorAction Stop).Source
  & $sevenZip x '-t#' (Join-Path $CacheRoot 'vc_redist.x64.exe') ('-o'+(Join-Path $CacheRoot 'vc-containers')) -y -bso0 -bsp0
  if ($LASTEXITCODE -ne 0) { throw 'VC++ container extraction failed' }
  & $sevenZip x (Join-Path $CacheRoot 'vc-containers/2.cab') ('-o'+(Join-Path $CacheRoot 'vc-manifest')) -y -bso0 -bsp0
  if ($LASTEXITCODE -ne 0) { throw 'VC++ manifest extraction failed' }
  & $sevenZip x (Join-Path $CacheRoot 'vc-containers/4.cab') ('-o'+(Join-Path $CacheRoot 'vc-payload')) -y -bso0 -bsp0
  if ($LASTEXITCODE -ne 0) { throw 'VC++ payload extraction failed' }
  [xml]$manifest = Get-Content -LiteralPath (Join-Path $CacheRoot 'vc-manifest/0')
  $cab = @($manifest.BurnManifest.Payload | Where-Object FilePath -eq 'packages\vcRuntimeMinimum_amd64\cab1.cab')
  if ($cab.Count -ne 1) { throw 'Cannot identify VC++ x64 runtime cabinet' }
  & $sevenZip x (Join-Path $CacheRoot ('vc-payload/'+$cab[0].SourcePath)) ('-o'+(Join-Path $CacheRoot 'vccrt')) -y -bso0 -bsp0
  if ($LASTEXITCODE -gt 1) { throw 'VC++ runtime extraction failed' }
}
