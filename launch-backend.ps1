[CmdletBinding()]
param()

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

if (-not (Test-Path $GoExe)) {
    throw "Go was not found at $GoExe. Update `$GoExe in this script before launching the backend."
}

Write-Host "Starting Sparkle Transcoder backend"
Write-Host "Media root: $env:MEDIA_ROOT"
Write-Host "Output: $env:OUTPUT"

Push-Location $RepoRoot
try {
    & $GoExe run ./cmd/server
}
finally {
    Pop-Location
}
