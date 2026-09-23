[CmdletBinding()]
param([switch]$ShowLogs)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$RepoRoot = (Resolve-Path -LiteralPath $PSScriptRoot).Path
$AppPath = Join-Path $RepoRoot 'bin\windows\Sparkle.exe'
if (-not (Test-Path -LiteralPath $AppPath)) {
    & (Join-Path $RepoRoot 'build-windows-app.ps1')
}
$arguments = "--repo-root `"$RepoRoot`""
if ($ShowLogs) { $arguments += ' --logs' }

# Compatibility entry point. Startup/Start Menu shortcuts target the GUI
# executable directly, so they never create a PowerShell console.
Start-Process -FilePath $AppPath -ArgumentList $arguments -WorkingDirectory $RepoRoot -WindowStyle Hidden
