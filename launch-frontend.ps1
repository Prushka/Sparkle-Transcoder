[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$RepoRoot = (Resolve-Path $PSScriptRoot).Path
$WebRoot = Join-Path $RepoRoot "web"
$NpmExe = "C:\Roxy\SDK\nodejs\npm.cmd"

# Frontend runtime environment.
$env:NEXT_PUBLIC_API_BASE = "http://localhost:1323/api"

if (-not (Test-Path $NpmExe)) {
    throw "npm was not found at $NpmExe. Update `$NpmExe in this script before launching the frontend."
}

Write-Host "Starting Sparkle Transcoder frontend on http://localhost:3000"
Write-Host "API base: $env:NEXT_PUBLIC_API_BASE"

Push-Location $WebRoot
try {
    if (-not (Test-Path (Join-Path $WebRoot "node_modules"))) {
        Write-Host "Frontend dependencies are missing; running npm install first."
        & $NpmExe install
    }

    & $NpmExe run dev
}
finally {
    Pop-Location
}
