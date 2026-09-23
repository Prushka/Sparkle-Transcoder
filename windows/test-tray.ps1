[CmdletBinding()]
param()
Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
$Compiler = Join-Path $env:SystemRoot "Microsoft.NET\Framework64\v4.0.30319\csc.exe"
if (-not (Test-Path -LiteralPath $Compiler)) { $Compiler = Join-Path $env:SystemRoot "Microsoft.NET\Framework\v4.0.30319\csc.exe" }
$TestRoot = Join-Path $RepoRoot ("tmp\tray test " + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $TestRoot | Out-Null
Copy-Item -LiteralPath (Join-Path $RepoRoot "launch-backend.ps1") -Destination $TestRoot
$FakeBackend = Join-Path $TestRoot "FakeBackend.exe"
$TestApp = Join-Path $TestRoot "TrayTests.exe"
& $Compiler /nologo /target:exe "/out:$FakeBackend" "$PSScriptRoot\tests\FakeBackend.cs"
if ($LASTEXITCODE -ne 0) { throw "Fake backend compilation failed." }
& $Compiler /nologo /target:winexe /main:Sparkle.Windows.TrayTests `
    /reference:System.dll /reference:System.Core.dll /reference:System.Drawing.dll /reference:System.Windows.Forms.dll `
    "/out:$TestApp" "$PSScriptRoot\Sparkle.cs" "$PSScriptRoot\tests\TrayTests.cs"
if ($LASTEXITCODE -ne 0) { throw "Tray test compilation failed." }
$process = Start-Process -FilePath $TestApp -ArgumentList "`"$TestRoot`" `"$FakeBackend`"" -WindowStyle Hidden -PassThru
if (-not $process.WaitForExit(60000)) {
    $process.Kill()
    throw "Tray integration tests timed out. Logs: $TestRoot"
}
$result = Get-Content -LiteralPath (Join-Path $TestRoot "test-result.txt") -Raw
Write-Host $result
if ($process.ExitCode -ne 0 -or -not $result.StartsWith("PASS:")) { throw "Tray integration tests failed. Logs: $TestRoot" }
