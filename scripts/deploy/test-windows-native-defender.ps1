param([Parameter(Mandatory)][string]$ScanPath)
$ErrorActionPreference = 'Stop'
if (!$IsWindows) { throw 'Defender validation requires Windows.' }
$status = Get-MpComputerStatus -ErrorAction Stop
if (!$status.AntivirusEnabled -or !$status.RealTimeProtectionEnabled) {
  throw 'Defender antivirus and real-time protection must be enabled for release validation.'
}
$defender = Join-Path $env:ProgramData 'Microsoft/Windows Defender/Platform/CurrentVersion/MpCmdRun.exe'
if (!(Test-Path -LiteralPath $defender)) { $defender = Join-Path $env:ProgramFiles 'Windows Defender/MpCmdRun.exe' }
$resolvedScanPath = (Resolve-Path -LiteralPath $ScanPath).Path
# Keep detections unremediated so exit 0 cannot mean a detection was removed.
& $defender -Scan -ScanType 3 -DisableRemediation -File $resolvedScanPath
if ($LASTEXITCODE -ne 0) { throw "Defender validation failed: $resolvedScanPath" }
