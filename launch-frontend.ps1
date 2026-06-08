[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$RepoRoot = (Resolve-Path $PSScriptRoot).Path
$WebRoot = Join-Path $RepoRoot "web"
$NpmExe = "C:\Roxy\SDK\nodejs\npm.cmd"
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

# Frontend runtime environment.
$env:SPARKLE_API_BASE = "http://localhost:1323/api"

if (-not (Test-Path $NpmExe)) {
    throw "npm was not found at $NpmExe. Update `$NpmExe in this script before launching the frontend."
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
        Write-Host "Frontend dependencies are missing; running npm install first."
        & $NpmExe install
    }

    Stop-StaleNextDevWorkers -Root $WebRoot
    Clear-NextDevCache -Root $WebRoot

    & $NpmExe run dev
}
finally {
    Pop-Location
}
