[CmdletBinding()]
param(
    [switch]$Remove,
    [switch]$NoStartup,
    [switch]$NoStartMenu,
    [string]$GoExe
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$RepoRoot = (Resolve-Path $PSScriptRoot).Path
$AppName = "Sparkle"
$AppPath = Join-Path $RepoRoot "bin\windows\Sparkle.exe"

$ShortcutName = "$AppName.lnk"
$LegacyShortcutNames = @("Sparkle Transcoder Backend.lnk")
$StartupDir = Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs\Startup"
$StartMenuDir = Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs"
$StartupShortcut = Join-Path $StartupDir $ShortcutName
$StartMenuShortcut = Join-Path $StartMenuDir $ShortcutName

function Remove-ShortcutIfPresent {
    param([string]$Path)

    if (Test-Path $Path) {
        Remove-Item -LiteralPath $Path -Force
        Write-Host "Removed $Path"
    }
}

function Remove-LegacyShortcuts {
    foreach ($name in $LegacyShortcutNames) {
        Remove-ShortcutIfPresent -Path (Join-Path $StartupDir $name)
        Remove-ShortcutIfPresent -Path (Join-Path $StartMenuDir $name)
    }
}

function New-BackendShortcut {
    param([string]$Path)

    if (-not (Test-Path -LiteralPath $AppPath)) { throw "Windows application not found at $AppPath" }

    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Path) | Out-Null

    $shell = New-Object -ComObject WScript.Shell
    $shortcut = $shell.CreateShortcut($Path)
    $shortcut.TargetPath = $AppPath
    $shortcut.Arguments = "--repo-root `"$RepoRoot`""
    $shortcut.WorkingDirectory = $RepoRoot
    $shortcut.Description = "Start and manage Sparkle."
    $shortcut.IconLocation = "$AppPath,0"
    $shortcut.Save()

    Write-Host "Created $Path"
}

if ($Remove) {
    Remove-ShortcutIfPresent -Path $StartupShortcut
    Remove-ShortcutIfPresent -Path $StartMenuShortcut
    Remove-LegacyShortcuts
    return
}

& (Join-Path $RepoRoot "build-windows-app.ps1") -GoExe $GoExe
Remove-LegacyShortcuts

if (-not $NoStartup) {
    New-BackendShortcut -Path $StartupShortcut
}

if (-not $NoStartMenu) {
    New-BackendShortcut -Path $StartMenuShortcut
}

Write-Host ""
Write-Host "Sparkle shortcuts updated. Double-click the tray icon to view logs; use its Quit menu to exit."
Write-Host "For a taskbar launcher, open Start, search '$AppName', right-click it, and choose 'Pin to taskbar'."
