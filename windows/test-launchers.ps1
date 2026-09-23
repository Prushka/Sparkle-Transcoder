[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$RepoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$TestRoot = Join-Path $RepoRoot ('tmp\launcher test ' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $TestRoot, (Join-Path $TestRoot 'web') | Out-Null
foreach ($name in @('launch-backend.ps1', 'launch-frontend.ps1')) {
    Copy-Item -LiteralPath (Join-Path $RepoRoot $name) -Destination $TestRoot
}

# No real backend, package installation, listener, or dev worker is touched.
function Get-NetTCPConnection { param($LocalPort, $State, $ErrorAction) return $null }
function Get-CimInstance { param($ClassName, $Filter) return @() }
function Assert-Contains {
    param([string]$Text, [string]$Expected)
    if (-not $Text.Contains($Expected)) { throw "Expected fixture output to contain: $Expected`n$Text" }
}

$FakeTool = Join-Path $TestRoot 'fake tool.cmd'
@'
@echo off
echo args=%*>>"%SPARKLE_TEST_RECORD%"
echo media=%MEDIA_ROOT%>>"%SPARKLE_TEST_RECORD%"
echo output=%OUTPUT%>>"%SPARKLE_TEST_RECORD%"
echo api=%SPARKLE_API_BASE%>>"%SPARKLE_TEST_RECORD%"
exit /b %SPARKLE_TEST_EXIT%
'@ | Set-Content -LiteralPath $FakeTool -Encoding ASCII

$Names = @('MEDIA_ROOT', 'OUTPUT', 'SPARKLE_API_BASE', 'SPARKLE_START_EVENT', 'SPARKLE_TEST_RECORD', 'SPARKLE_TEST_EXIT')
$Saved = @{}
foreach ($name in $Names) { $Saved[$name] = [Environment]::GetEnvironmentVariable($name, 'Process') }
try {
    $env:SPARKLE_START_EVENT = ''
    $env:SPARKLE_TEST_EXIT = '0'
    $env:SPARKLE_TEST_RECORD = Join-Path $TestRoot 'commands.txt'
    $env:MEDIA_ROOT = ''
    $env:OUTPUT = ''
    & (Join-Path $TestRoot 'launch-backend.ps1') -NoLocalConfig -GoExe $FakeTool
    $record = Get-Content -LiteralPath $env:SPARKLE_TEST_RECORD -Raw
    Assert-Contains $record 'args=run ./cmd/server'
    Assert-Contains $record ('media=' + (Join-Path $TestRoot 'media'))
    Assert-Contains $record ('output=' + (Join-Path $TestRoot 'output'))

    $env:MEDIA_ROOT = Join-Path $TestRoot 'caller media'
    $env:OUTPUT = Join-Path $TestRoot 'caller output'
    & (Join-Path $TestRoot 'launch-backend.ps1') -NoLocalConfig -BackendExecutable $FakeTool -GoExe 'missing-go'
    $record = Get-Content -LiteralPath $env:SPARKLE_TEST_RECORD -Raw
    Assert-Contains $record ('media=' + $env:MEDIA_ROOT)
    Assert-Contains $record ('output=' + $env:OUTPUT)

    @'
param([string]$BackendExecutable, [string]$GoExe = 'missing-go')
$env:MEDIA_ROOT = Join-Path $PSScriptRoot 'local media'
& (Join-Path $PSScriptRoot 'launch-backend.ps1') -NoLocalConfig -GoExe $GoExe -BackendExecutable $BackendExecutable
'@ | Set-Content -LiteralPath (Join-Path $TestRoot 'launch-backend.local.ps1') -Encoding UTF8
    & (Join-Path $TestRoot 'launch-backend.ps1') -BackendExecutable $FakeTool
    & (Join-Path $TestRoot 'launch-backend.ps1') -GoExe $FakeTool
    Assert-Contains (Get-Content -LiteralPath $env:SPARKLE_TEST_RECORD -Raw) ('media=' + (Join-Path $TestRoot 'local media'))

    $env:MEDIA_ROOT = Join-Path $TestRoot 'bypass media'
    & (Join-Path $TestRoot 'launch-backend.ps1') -NoLocalConfig -BackendExecutable $FakeTool
    if ($env:MEDIA_ROOT -ne (Join-Path $TestRoot 'bypass media')) { throw 'NoLocalConfig did not bypass the local backend launcher.' }

    $env:SPARKLE_API_BASE = 'http://127.0.0.1:4321/api'
    & (Join-Path $TestRoot 'launch-frontend.ps1') -NoLocalConfig -NpmExe $FakeTool
    $record = Get-Content -LiteralPath $env:SPARKLE_TEST_RECORD -Raw
    Assert-Contains $record 'args=ci'
    Assert-Contains $record 'args=run dev'
    Assert-Contains $record 'api=http://127.0.0.1:4321/api'
    @'
param([string]$NpmExe)
$env:SPARKLE_API_BASE = 'http://127.0.0.1:4322/api'
& (Join-Path $PSScriptRoot 'launch-frontend.ps1') -NoLocalConfig -NpmExe $NpmExe
'@ | Set-Content -LiteralPath (Join-Path $TestRoot 'launch-frontend.local.ps1') -Encoding UTF8
    & (Join-Path $TestRoot 'launch-frontend.ps1') -NpmExe $FakeTool
    Assert-Contains (Get-Content -LiteralPath $env:SPARKLE_TEST_RECORD -Raw) 'api=http://127.0.0.1:4322/api'

    $env:SPARKLE_TEST_EXIT = '7'
    $failed = $false
    try { & (Join-Path $TestRoot 'launch-backend.ps1') -BackendExecutable $FakeTool }
    catch { $failed = $_.Exception.Message.Contains('code 7') }
    if (-not $failed) { throw 'Backend failure was not propagated through the local launcher.' }
    $failed = $false
    try { & (Join-Path $TestRoot 'launch-frontend.ps1') -NpmExe $FakeTool }
    catch { $failed = $_.Exception.Message.Contains('installation failed') }
    if (-not $failed) { throw 'Frontend dependency failure did not stop startup.' }

    Write-Host 'PASS: portable defaults, caller overrides, compiled backend without Go, local delegation/bypass, npm arguments, and failure propagation.'
}
finally {
    foreach ($name in $Names) { [Environment]::SetEnvironmentVariable($name, $Saved[$name], 'Process') }
}
