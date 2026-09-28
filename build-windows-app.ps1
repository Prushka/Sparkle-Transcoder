[CmdletBinding()]
param(
    [string]$GoExe,
    [string]$OutputDirectory,
    [switch]$BackendOnly
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
# The tray owns compiler children before this script is allowed to start them.
if ($env:SPARKLE_BUILD_EVENT) {
    $gate = [Threading.EventWaitHandle]::OpenExisting($env:SPARKLE_BUILD_EVENT)
    try {
        if (-not $gate.WaitOne(30000)) { throw 'Tray build startup timed out.' }
    } finally { $gate.Dispose() }
}
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
$RepoRoot = (Resolve-Path -LiteralPath $PSScriptRoot).Path
if (-not $OutputDirectory) { $OutputDirectory = Join-Path $RepoRoot "bin\windows" }
$OutputDirectory = [System.IO.Path]::GetFullPath($OutputDirectory)
if (-not $GoExe) {
    $command = Get-Command go -CommandType Application -ErrorAction SilentlyContinue
    if ($command) { $GoExe = $command.Source }
    else { throw "Go was not found on PATH. Supply -GoExe with the path to go.exe." }
}
$GoExe = (Get-Command $GoExe -CommandType Application -ErrorAction Stop).Source
if (-not $BackendOnly) {
    $Compiler = Join-Path $env:SystemRoot "Microsoft.NET\Framework64\v4.0.30319\csc.exe"
    if (-not (Test-Path -LiteralPath $Compiler)) {
        $Compiler = Join-Path $env:SystemRoot "Microsoft.NET\Framework\v4.0.30319\csc.exe"
    }
    if (-not (Test-Path -LiteralPath $Compiler)) { throw "The Windows .NET Framework C# compiler is required." }
}

$AppPath = Join-Path $OutputDirectory "Sparkle.exe"
$BackendPath = Join-Path $OutputDirectory "Sparkle.Backend.exe"
$targets = @($BackendPath)
if (-not $BackendOnly) { $targets += $AppPath }
$running = @(Get-Process -Name Sparkle,Sparkle.Backend -ErrorAction SilentlyContinue | Where-Object { $_.Path -in $targets })
if ($running.Count) { throw "Quit Sparkle Transcoder from its tray menu before rebuilding the Windows application." }

$Stage = if ($BackendOnly) { $OutputDirectory } else { Join-Path $RepoRoot ("tmp\windows-build-" + [guid]::NewGuid().ToString("N")) }
New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null
if ($Stage -ne $OutputDirectory) { New-Item -ItemType Directory -Force -Path $Stage | Out-Null }
$StagedApp = Join-Path $Stage "Sparkle.exe"
$StagedBackend = Join-Path $Stage "Sparkle.Backend.exe"

Push-Location $RepoRoot
try {
    & $GoExe build -trimpath -o $StagedBackend ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw "Backend build failed." }
    if (-not $BackendOnly) {
        & $Compiler /nologo /target:winexe /optimize+ /platform:anycpu `
            /reference:System.dll /reference:System.Core.dll /reference:System.Drawing.dll /reference:System.Windows.Forms.dll `
            "/win32icon:$RepoRoot\assets\sparkle-transcoder.ico" "/out:$StagedApp" "$RepoRoot\windows\Sparkle.cs"
        if ($LASTEXITCODE -ne 0) { throw "Windows tray application build failed." }
        Copy-Item -LiteralPath $StagedBackend -Destination $BackendPath -Force
        Copy-Item -LiteralPath $StagedApp -Destination $AppPath -Force
        # Keep custom -GoExe installations usable from Explorer's tray environment.
        [IO.File]::WriteAllText((Join-Path $OutputDirectory 'Sparkle.Go.txt'), $GoExe)
    }
}
finally { Pop-Location }

if ($BackendOnly) { Write-Host 'Backend build completed.' }
else { Write-Host "Built $AppPath" }
