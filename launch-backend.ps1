[CmdletBinding()]
param(
    [string]$BackendExecutable
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$RepoRoot = (Resolve-Path $PSScriptRoot).Path
$GoExe = "C:\Roxy\SDK\go1.26.2\bin\go.exe"

# Backend runtime environment. Edit these values here when the media mount or tools move.
$env:MEDIA_ROOT = "O:\Managed-Videos"
$env:OUTPUT = "O:\Managed-Videos\Public\output"

$env:SVT_AV1_ENCODER = "nvenc_av1_10bit"
$env:AV1_PRESET = "slowest"
$env:TASK_CONCURRENCY = "3"

if (-not $BackendExecutable -and -not (Test-Path $GoExe)) {
    throw "Go was not found at $GoExe. Update `$GoExe in this script before launching the backend."
}

Write-Host "Starting Sparkle Transcoder backend"
Write-Host "Media root: $env:MEDIA_ROOT"
Write-Host "Output: $env:OUTPUT"

Push-Location $RepoRoot
try {
    if ($BackendExecutable) {
        if (-not (Test-Path -LiteralPath $BackendExecutable -PathType Leaf)) {
            throw "Backend executable not found at $BackendExecutable. Run build-windows-app.ps1."
        }
        # The tray assigns this process to its job before allowing children to
        # start, so Quit/crash cleanup cannot leave orphan encoder processes.
        if ($env:SPARKLE_START_EVENT) {
            $startGate = [System.Threading.EventWaitHandle]::OpenExisting($env:SPARKLE_START_EVENT)
            try {
                if (-not $startGate.WaitOne(30000)) { throw "Tray startup timed out." }
            }
            finally { $startGate.Dispose() }
        }
        [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
        & $BackendExecutable
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    }
    else {
        & $GoExe run ./cmd/server
    }
}
finally {
    Pop-Location
}
