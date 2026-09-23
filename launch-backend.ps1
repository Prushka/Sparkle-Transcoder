[CmdletBinding()]
param(
    [string]$BackendExecutable,
    [string]$GoExe = "go",
    [switch]$NoLocalConfig
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$RepoRoot = (Resolve-Path $PSScriptRoot).Path
$LocalLauncher = Join-Path $RepoRoot "launch-backend.local.ps1"
if (-not $NoLocalConfig -and (Test-Path -LiteralPath $LocalLauncher)) {
    $LocalArguments = @{} + $PSBoundParameters
    $LocalArguments.Remove('NoLocalConfig')
    & $LocalLauncher @LocalArguments
    return
}

# Caller-provided environment values take precedence over portable defaults.
if (-not $env:MEDIA_ROOT) { $env:MEDIA_ROOT = Join-Path $RepoRoot "media" }
if (-not $env:OUTPUT) { $env:OUTPUT = Join-Path $RepoRoot "output" }

if (-not $BackendExecutable -and -not (Get-Command $GoExe -CommandType Application -ErrorAction SilentlyContinue)) {
    throw "Go was not found. Install Go on PATH or supply -GoExe with its executable path."
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
        if ($LASTEXITCODE -ne 0) { throw "Backend exited with code $LASTEXITCODE." }
    }
    else {
        & $GoExe run ./cmd/server
        if ($LASTEXITCODE -ne 0) { throw "Backend exited with code $LASTEXITCODE." }
    }
}
finally {
    Pop-Location
}
