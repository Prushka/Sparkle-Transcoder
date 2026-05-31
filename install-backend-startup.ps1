[CmdletBinding()]
param(
    [switch]$Remove,
    [switch]$NoStartup,
    [switch]$NoStartMenu
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$RepoRoot = (Resolve-Path $PSScriptRoot).Path
$AppName = "Sparkle"
$TrayScript = Join-Path $RepoRoot "launch-backend-tray.ps1"
$IconPath = Join-Path $RepoRoot "assets\sparkle-transcoder.ico"
$PowerShellExe = Join-Path $env:SystemRoot "System32\WindowsPowerShell\v1.0\powershell.exe"

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

    if (-not (Test-Path $TrayScript)) {
        throw "Tray launcher not found at $TrayScript"
    }

    if (-not (Test-Path $IconPath)) {
        throw "App icon not found at $IconPath"
    }

    if (-not (Test-Path $PowerShellExe)) {
        throw "Windows PowerShell not found at $PowerShellExe"
    }

    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Path) | Out-Null

    $shell = New-Object -ComObject WScript.Shell
    $shortcut = $shell.CreateShortcut($Path)
    $shortcut.TargetPath = $PowerShellExe
    $shortcut.Arguments = "-NoProfile -ExecutionPolicy Bypass -STA -WindowStyle Hidden -File `"$TrayScript`""
    $shortcut.WorkingDirectory = $RepoRoot
    $shortcut.Description = "Start and manage Sparkle."
    $shortcut.IconLocation = "$IconPath,0"
    $shortcut.Save()

    Write-Host "Created $Path"
}

if ($Remove) {
    Remove-ShortcutIfPresent -Path $StartupShortcut
    Remove-ShortcutIfPresent -Path $StartMenuShortcut
    Remove-LegacyShortcuts
    return
}

Remove-LegacyShortcuts

if (-not $NoStartup) {
    New-BackendShortcut -Path $StartupShortcut
}

if (-not $NoStartMenu) {
    New-BackendShortcut -Path $StartMenuShortcut
}

Write-Host ""
Write-Host "Startup shortcut installed. The tray controller will launch when you sign in."
Write-Host "For a taskbar launcher, open Start, search '$AppName', right-click it, and choose 'Pin to taskbar'."
