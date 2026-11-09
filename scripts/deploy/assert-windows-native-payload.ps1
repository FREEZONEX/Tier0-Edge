param([Parameter(Mandatory)][string]$PayloadRoot)
$ErrorActionPreference='Stop'
$root=(Resolve-Path -LiteralPath $PayloadRoot).Path
if ($IsLinux) {
 $link=Get-ChildItem -LiteralPath $root -Force -Recurse | Where-Object LinkType -eq 'SymbolicLink' | Select-Object -First 1
 if($link){throw "Symbolic links cannot enter the Windows payload: $($link.FullName)"}
}
$files=@(Get-ChildItem -LiteralPath $root -File -Recurse | Where-Object Name -notin @('SIZE-REPORT.json','FILES.txt'))
$total=0L
$components=@{}
foreach($file in $files) {
 $relative=[IO.Path]::GetRelativePath($root,$file.FullName)
 $component=($relative -split '[\\/]')[0]
 if(!$components.ContainsKey($component)){$components[$component]=0L}
 $components[$component]+=$file.Length
 $total+=$file.Length
 $stream=[IO.File]::OpenRead($file.FullName)
 try {
  $header=[byte[]]::new(4)
  if($stream.Read($header,0,4) -ne 4){continue}
  $magic=[BitConverter]::ToUInt32($header,0)
  if($magic.ToString('x8') -in @('464c457f','feedface','cefaedfe','feedfacf','cffaedfe','cafebabe','bebafeca')) {
   throw "Non-Windows binary found in Windows payload: $relative"
  }
  if($file.Extension.ToLowerInvariant() -in @('.exe','.dll','.node')) {
   if($header[0] -ne 0x4d -or $header[1] -ne 0x5a){throw "Invalid Windows executable: $relative"}
   $reader=[IO.BinaryReader]::new($stream,[Text.Encoding]::UTF8,$true)
   $stream.Position=0x3c
   $peOffset=$reader.ReadUInt32()
   if($peOffset -gt $stream.Length-6){throw "Invalid PE header: $relative"}
   $stream.Position=$peOffset
   if($reader.ReadUInt32() -ne 0x4550 -or $reader.ReadUInt16() -ne 0x8664){throw "Non-x64 executable in Windows x64 payload: $relative"}
  }
 } finally {$stream.Dispose()}
}
$rows=@($components.GetEnumerator() | Sort-Object Value -Descending | ForEach-Object { @{component=$_.Key;bytes=$_.Value} })
$report=@{platform='windows';architecture='amd64';fileCount=$files.Count;uncompressedBytes=$total;components=$rows}
[IO.File]::WriteAllText((Join-Path $root 'SIZE-REPORT.json'),($report|ConvertTo-Json -Depth 5))
Write-Output "Windows x64 payload verified: $($files.Count) files, $([math]::Round($total/1MB,1)) MiB uncompressed."
