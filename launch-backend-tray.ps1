[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing

$CreatedNew = $false
$Mutex = [System.Threading.Mutex]::new($true, "Local\SparkleTranscoderBackendTray", [ref]$CreatedNew)
if (-not $CreatedNew) {
    [System.Windows.Forms.MessageBox]::Show(
        "Sparkle Transcoder backend tray is already running.",
        "Sparkle Transcoder",
        [System.Windows.Forms.MessageBoxButtons]::OK,
        [System.Windows.Forms.MessageBoxIcon]::Information
    ) | Out-Null
    return
}

$script:RepoRoot = (Resolve-Path $PSScriptRoot).Path
$script:BackendScript = Join-Path $script:RepoRoot "launch-backend.ps1"
$script:LogDir = Join-Path $script:RepoRoot ".sparkle-transcoder\logs"
$script:StdoutLog = Join-Path $script:LogDir "backend.out.log"
$script:StderrLog = Join-Path $script:LogDir "backend.err.log"
$script:PowerShellExe = Join-Path $env:SystemRoot "System32\WindowsPowerShell\v1.0\powershell.exe"
$script:BackendProcess = $null
$script:NotifyIcon = $null
$script:StatusItem = $null
$script:StartItem = $null
$script:StopItem = $null
$script:RestartItem = $null
$script:Timer = $null

function Show-TrayBalloon {
    param(
        [string]$Title,
        [string]$Message,
        [System.Windows.Forms.ToolTipIcon]$Icon = [System.Windows.Forms.ToolTipIcon]::Info
    )

    if ($null -ne $script:NotifyIcon) {
        $script:NotifyIcon.ShowBalloonTip(3000, $Title, $Message, $Icon)
    }
}

function Get-BackendProcess {
    if ($null -eq $script:BackendProcess) {
        return $null
    }

    try {
        $script:BackendProcess.Refresh()
        if (-not $script:BackendProcess.HasExited) {
            return $script:BackendProcess
        }
    }
    catch {
        # The process may have exited between refreshes.
    }

    $script:BackendProcess = $null
    return $null
}

function Update-TrayState {
    $process = Get-BackendProcess
    $running = $null -ne $process

    if ($running) {
        $script:StatusItem.Text = "Status: Running (PID $($process.Id))"
        $script:NotifyIcon.Text = "Sparkle Backend: Running"
        $script:NotifyIcon.Icon = [System.Drawing.SystemIcons]::Application
    }
    else {
        $script:StatusItem.Text = "Status: Stopped"
        $script:NotifyIcon.Text = "Sparkle Backend: Stopped"
        $script:NotifyIcon.Icon = [System.Drawing.SystemIcons]::Warning
    }

    $script:StartItem.Enabled = -not $running
    $script:StopItem.Enabled = $running
    $script:RestartItem.Enabled = $running
}

function Stop-ProcessTree {
    param([int]$ProcessId)

    $children = @(Get-CimInstance Win32_Process -Filter "ParentProcessId=$ProcessId" -ErrorAction SilentlyContinue)
    foreach ($child in $children) {
        Stop-ProcessTree -ProcessId ([int]$child.ProcessId)
    }

    $process = Get-Process -Id $ProcessId -ErrorAction SilentlyContinue
    if ($null -ne $process) {
        Stop-Process -Id $ProcessId -Force -ErrorAction SilentlyContinue
    }
}

function Start-Backend {
    if ($null -ne (Get-BackendProcess)) {
        Update-TrayState
        return
    }

    if (-not (Test-Path $script:BackendScript)) {
        Show-TrayBalloon "Sparkle Transcoder" "Cannot find launch-backend.ps1." ([System.Windows.Forms.ToolTipIcon]::Error)
        return
    }

    if (-not (Test-Path $script:PowerShellExe)) {
        Show-TrayBalloon "Sparkle Transcoder" "Cannot find Windows PowerShell." ([System.Windows.Forms.ToolTipIcon]::Error)
        return
    }

    New-Item -ItemType Directory -Force -Path $script:LogDir | Out-Null

    $arguments = @(
        "-NoProfile",
        "-ExecutionPolicy", "Bypass",
        "-File", "`"$script:BackendScript`""
    )

    try {
        $script:BackendProcess = Start-Process `
            -FilePath $script:PowerShellExe `
            -ArgumentList $arguments `
            -WorkingDirectory $script:RepoRoot `
            -WindowStyle Hidden `
            -RedirectStandardOutput $script:StdoutLog `
            -RedirectStandardError $script:StderrLog `
            -PassThru

        Update-TrayState
        Show-TrayBalloon "Sparkle Transcoder" "Backend started." ([System.Windows.Forms.ToolTipIcon]::Info)
    }
    catch {
        $script:BackendProcess = $null
        Update-TrayState
        Show-TrayBalloon "Sparkle Transcoder" "Backend failed to start. See the log folder." ([System.Windows.Forms.ToolTipIcon]::Error)
    }
}

function Stop-Backend {
    $process = Get-BackendProcess
    if ($null -eq $process) {
        Update-TrayState
        return
    }

    Stop-ProcessTree -ProcessId $process.Id
    $script:BackendProcess = $null
    Update-TrayState
    Show-TrayBalloon "Sparkle Transcoder" "Backend stopped." ([System.Windows.Forms.ToolTipIcon]::Info)
}

function Restart-Backend {
    Stop-Backend
    Start-Sleep -Milliseconds 500
    Start-Backend
}

function Open-LogFolder {
    New-Item -ItemType Directory -Force -Path $script:LogDir | Out-Null
    Start-Process explorer.exe -ArgumentList "`"$script:LogDir`""
}

try {
    [System.Windows.Forms.Application]::EnableVisualStyles()

    $script:NotifyIcon = [System.Windows.Forms.NotifyIcon]::new()
    $script:NotifyIcon.Icon = [System.Drawing.SystemIcons]::Application
    $script:NotifyIcon.Text = "Sparkle Backend"
    $script:NotifyIcon.Visible = $true

    $menu = [System.Windows.Forms.ContextMenuStrip]::new()

    $script:StatusItem = [System.Windows.Forms.ToolStripMenuItem]::new("Status: Starting")
    $script:StatusItem.Enabled = $false
    [void]$menu.Items.Add($script:StatusItem)
    [void]$menu.Items.Add([System.Windows.Forms.ToolStripSeparator]::new())

    $script:StartItem = [System.Windows.Forms.ToolStripMenuItem]::new("Start Backend")
    $script:StartItem.Add_Click({ Start-Backend })
    [void]$menu.Items.Add($script:StartItem)

    $script:StopItem = [System.Windows.Forms.ToolStripMenuItem]::new("Stop Backend")
    $script:StopItem.Add_Click({ Stop-Backend })
    [void]$menu.Items.Add($script:StopItem)

    $script:RestartItem = [System.Windows.Forms.ToolStripMenuItem]::new("Restart Backend")
    $script:RestartItem.Add_Click({ Restart-Backend })
    [void]$menu.Items.Add($script:RestartItem)

    [void]$menu.Items.Add([System.Windows.Forms.ToolStripSeparator]::new())

    $openLogsItem = [System.Windows.Forms.ToolStripMenuItem]::new("Open Logs")
    $openLogsItem.Add_Click({ Open-LogFolder })
    [void]$menu.Items.Add($openLogsItem)

    [void]$menu.Items.Add([System.Windows.Forms.ToolStripSeparator]::new())

    $exitItem = [System.Windows.Forms.ToolStripMenuItem]::new("Quit")
    $exitItem.Add_Click({
        Stop-Backend
        [System.Windows.Forms.Application]::ExitThread()
    })
    [void]$menu.Items.Add($exitItem)

    $script:NotifyIcon.ContextMenuStrip = $menu

    $script:Timer = [System.Windows.Forms.Timer]::new()
    $script:Timer.Interval = 3000
    $script:Timer.Add_Tick({ Update-TrayState })
    $script:Timer.Start()

    Start-Backend
    Update-TrayState

    [System.Windows.Forms.Application]::Run()
}
finally {
    if ($null -ne $script:Timer) {
        $script:Timer.Stop()
        $script:Timer.Dispose()
    }

    if ($null -ne $script:NotifyIcon) {
        $script:NotifyIcon.Visible = $false
        $script:NotifyIcon.Dispose()
    }

    if ($CreatedNew) {
        $Mutex.ReleaseMutex()
    }
    $Mutex.Dispose()
}
