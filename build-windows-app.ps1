[CmdletBinding()]
param(
    [string]$GoExe,
    [string]$OutputDirectory
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path -LiteralPath $PSScriptRoot).Path
if (-not $OutputDirectory) { $OutputDirectory = Join-Path $RepoRoot "bin\windows" }
$OutputDirectory = [System.IO.Path]::GetFullPath($OutputDirectory)
if (-not $GoExe) {
    $command = Get-Command go -ErrorAction SilentlyContinue
    if ($command) { $GoExe = $command.Source }
    else { throw "Go was not found on PATH. Supply -GoExe with the path to go.exe." }
}
$Compiler = Join-Path $env:SystemRoot "Microsoft.NET\Framework64\v4.0.30319\csc.exe"
if (-not (Test-Path -LiteralPath $Compiler)) {
    $Compiler = Join-Path $env:SystemRoot "Microsoft.NET\Framework\v4.0.30319\csc.exe"
}
if (-not (Test-Path -LiteralPath $Compiler)) { throw "The Windows .NET Framework C# compiler is required." }

$AppPath = Join-Path $OutputDirectory "Sparkle.exe"
$running = @(Get-Process -Name Sparkle -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $AppPath })
if ($running.Count) { throw "Quit Sparkle from its tray menu before rebuilding the Windows application." }

$Stage = Join-Path $RepoRoot ("tmp\windows-build-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $Stage, $OutputDirectory | Out-Null
$StagedApp = Join-Path $Stage "Sparkle.exe"
$StagedBackend = Join-Path $Stage "Sparkle.Backend.exe"

Push-Location $RepoRoot
try {
    & $GoExe build -trimpath -o $StagedBackend ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw "Backend build failed." }
    & $Compiler /nologo /target:winexe /optimize+ /platform:anycpu `
        /reference:System.dll /reference:System.Core.dll /reference:System.Drawing.dll /reference:System.Windows.Forms.dll `
        "/win32icon:$RepoRoot\assets\sparkle-transcoder.ico" "/out:$StagedApp" "$RepoRoot\windows\Sparkle.cs"
    if ($LASTEXITCODE -ne 0) { throw "Windows tray application build failed." }
    Copy-Item -LiteralPath $StagedBackend -Destination (Join-Path $OutputDirectory "Sparkle.Backend.exe") -Force
    Copy-Item -LiteralPath $StagedApp -Destination $AppPath -Force
}
finally { Pop-Location }

Write-Host "Built $AppPath"
