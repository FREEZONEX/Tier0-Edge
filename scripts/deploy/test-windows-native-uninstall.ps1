param()
$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
$build = Join-Path $repo ('backend/.build/uninstall-tests-'+[Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $build | Out-Null
foreach ($pair in @(@('windows-native-installer.go','main.go'),@('windows-native-upgrade.go','upgrade.go'),@('windows-native-installer_test.go','installer_test.go'))) {
  Copy-Item -LiteralPath (Join-Path $PSScriptRoot $pair[0]) -Destination (Join-Path $build $pair[1])
}
# Compilation requires the embed file; tests construct their own ZIP fixtures.
$archive = [IO.Compression.ZipFile]::Open((Join-Path $build 'payload.zip'),[IO.Compression.ZipArchiveMode]::Create)
$archive.Dispose()
Push-Location (Join-Path $repo 'backend')
try {
  & go test -count=1 ./internal/nativeinstall
  if ($LASTEXITCODE -ne 0) { throw 'Native uninstall safety tests failed' }
} finally { Pop-Location }
Push-Location $build
try {
  & go test -count=1 -ldflags '-X backend/internal/runtimeenv.BuildDeployment=native -X backend/internal/runtimeenv.BuildVersion=3.0.0 -X backend/internal/runtimeenv.BuildRevision=test-revision' main.go upgrade.go installer_test.go
  if ($LASTEXITCODE -ne 0) { throw 'Installer reinstall tests failed' }
} finally { Pop-Location }
