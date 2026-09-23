[CmdletBinding()]
param(
    [string]$NpmExe = "npm.cmd",
    [switch]$NoLocalConfig
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$RepoRoot = (Resolve-Path $PSScriptRoot).Path
$WebRoot = Join-Path $RepoRoot "web"
$LocalLauncher = Join-Path $RepoRoot "launch-frontend.local.ps1"
if (-not $NoLocalConfig -and (Test-Path -LiteralPath $LocalLauncher)) {
    $LocalArguments = @{} + $PSBoundParameters
    $LocalArguments.Remove('NoLocalConfig')
    & $LocalLauncher @LocalArguments
    return
}
$FrontendPort = 3000
 
function Stop-StaleNextDevWorkers {
    param([string]$Root)

    $WorkerPath = (Join-Path $Root ".next\dev\build\postcss.js").Replace("\", "/").ToLowerInvariant()
    Get-CimInstance Win32_Process -Filter "name = 'node.exe'" |
        Where-Object {
            if (-not $_.CommandLine) {
                return $false
            }

            $_.CommandLine.Replace("\", "/").ToLowerInvariant().Contains($WorkerPath)
        } |
        ForEach-Object {
            Write-Host "Stopping stale Next dev worker PID $($_.ProcessId)."
            Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
        }
}

function Clear-NextDevCache {
    param([string]$Root)

    $DevCache = Join-Path $Root ".next\dev"
    if (-not (Test-Path $DevCache)) {
        return
    }

    $ResolvedRoot = (Resolve-Path $Root).Path
    $ResolvedDevCache = (Resolve-Path $DevCache).Path
    $ExpectedDevCache = [System.IO.Path]::GetFullPath((Join-Path $ResolvedRoot ".next\dev"))

    if (-not [string]::Equals($ResolvedDevCache, $ExpectedDevCache, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing to delete unexpected Next dev cache path: $ResolvedDevCache"
    }

    Write-Host "Clearing Next dev cache at $ResolvedDevCache."
    Remove-Item -LiteralPath $ResolvedDevCache -Recurse -Force
}

# Use the caller's backend URL when provided.
if (-not $env:SPARKLE_API_BASE) { $env:SPARKLE_API_BASE = "http://localhost:1323/api" }

if (-not (Get-Command $NpmExe -CommandType Application -ErrorAction SilentlyContinue)) {
    throw "npm was not found. Install Node.js on PATH or supply -NpmExe with its executable path."
}

Write-Host "Starting Sparkle Transcoder frontend on http://localhost:$FrontendPort"
Write-Host "API base: $env:SPARKLE_API_BASE"

Push-Location $WebRoot
try {
    $ExistingListener = Get-NetTCPConnection -LocalPort $FrontendPort -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($ExistingListener) {
        Write-Host "Frontend is already listening on http://localhost:$FrontendPort (PID $($ExistingListener.OwningProcess))."
        return
    }

    if (-not (Test-Path (Join-Path $WebRoot "node_modules"))) {
        Write-Host "Frontend dependencies are missing; running npm ci first."
        & $NpmExe ci
        if ($LASTEXITCODE -ne 0) { throw "Frontend dependency installation failed." }
    }

    Stop-StaleNextDevWorkers -Root $WebRoot
    Clear-NextDevCache -Root $WebRoot

    & $NpmExe run dev
    if ($LASTEXITCODE -ne 0) { throw "Frontend exited with code $LASTEXITCODE." }
}
finally {
    Pop-Location
}
