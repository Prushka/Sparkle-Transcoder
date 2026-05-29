[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$RepoRoot = (Resolve-Path $PSScriptRoot).Path
$GoExe = "C:\Roxy\SDK\go1.26.2\bin\go.exe"

# Backend runtime environment. Edit these values here when the media mount or tools move.
$env:DEBUG = "false"
$env:API_ADDR = ":1323"
$env:MEDIA_ROOT = "O:\Managed-Videos"
$env:OUTPUT = "O:\Managed-Videos\Public\output"
$env:DATA_DIR = Join-Path $RepoRoot ".sparkle-transcoder"
$env:SCAN_CACHE_FILE = Join-Path $env:DATA_DIR "scan-cache.json"
$env:SCAN_INCREMENTAL = "true"
$env:SCAN_ON_STARTUP = "true"
$env:SCAN_INPUT_INTERVAL = "4h"
$env:MEDIA_LIBRARIES = "Anime,Anime-R,Movies,Movies-R,TV-Shows,TV-Shows-R,Public/input"
$env:MEDIA_EXCLUDE_DIRS = "Public/output,Public/temp,.Trashes,.Spotlight-V100,.fseventsd"

$env:FFMPEG = "ffmpeg"
$env:FFPROBE = "ffprobe"
$env:MKVEXTRACT = "mkvextract"
$env:HANDBRAKE_CLI = "HandBrakeCLI"

$env:CONSTANT_QUALITY = "18"
$env:VIDEO_EXT = "mp4"
$env:ENCODER = "av1,hevc"
$env:AUDIO_KBPS = "144"
$env:SVT_AV1_ENCODER = "svt_av1_10bit"
$env:AV1_PRESET = "4"
$env:HEVC_ENCODER = "nvenc_h265_10bit"
$env:HEVC_PRESET = "slowest"
$env:H264_10BIT_ENCODER = "x264_10bit"
$env:H264_10BIT_PRESET = "slow"
$env:H264_ENCODER = "x264"
$env:H264_PRESET = "slow"
$env:H264_PROFILE = "baseline"
$env:H264_TUNE = "fastdecode"

$env:THUMBNAIL_HEIGHT = "320"
$env:THUMBNAIL_INTERVAL = "2"
$env:THUMBNAIL_CHUNK_INTERVAL = "1152"

$env:ENABLE_ENCODE = "true"
$env:ENABLE_SPRITE = "true"
$env:ENABLE_LOW_PRIORITY = "true"
$env:COMPUTE_SHA256 = "false"
$env:TASK_CONCURRENCY = "1"

if (-not (Test-Path $GoExe)) {
    throw "Go was not found at $GoExe. Update `$GoExe in this script before launching the backend."
}

Write-Host "Starting Sparkle Transcoder backend on $env:API_ADDR"
Write-Host "Media root: $env:MEDIA_ROOT"
Write-Host "Output: $env:OUTPUT"

Push-Location $RepoRoot
try {
    & $GoExe run ./cmd/server
}
finally {
    Pop-Location
}
