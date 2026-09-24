# Configuration and operations reference

For an overview and quick start, see the [README](../README.md).

- [Local setup](#run-locally)
- [Backend configuration](#backend-configuration)
- [Scanning and task behavior](#scanning-and-task-behavior)
- [API](#api)
- [Windows tray](#windows-startup-and-tray-launcher)
- [Docker and publishing](#docker-frontend-and-publishing)
- [Development checks](#development-and-verification)

## Run locally

Run commands from the repository root unless stated otherwise. The supplied
PowerShell launchers resolve SDKs from PATH and accept explicit executable paths.

### Backend

Set the source and output directories, then run the backend launcher:

```powershell
$env:MEDIA_ROOT = "D:\Media"
$env:OUTPUT = "D:\Transcodes"
./launch-backend.ps1 -NoLocalConfig
```

Without environment overrides, the launcher uses `media/` and `output/` under
the repository root. Supply `-GoExe <path>` if Go is not on PATH. The
`-NoLocalConfig` switch bypasses any ignored local launcher (described below).

To use Go on PATH and supply your own paths, launch the server directly:

```powershell
$env:MEDIA_ROOT = "D:\Media"
$env:OUTPUT = "D:\Transcodes"
go run ./cmd/server
```

The API listens on port 1323 by default. Check it at
[http://localhost:1323/api/health](http://localhost:1323/api/health).
On macOS/Linux, set the same environment variables and run `go run ./cmd/server`.
The backend uses environment variables; it does not load a `.env` file itself.

### Frontend

In a separate shell, install dependencies and start the dashboard:

```powershell
Set-Location web
npm ci
$env:SPARKLE_API_BASE = "http://localhost:1323/api"
npm run dev
```

Open [http://localhost:3000](http://localhost:3000). For a production server, run
`npm run build` and then `npm start` from `web`, with `SPARKLE_API_BASE` set in
that server's environment.

The Windows convenience launcher `./launch-frontend.ps1` also starts development
mode. It uses `npm.cmd` on PATH (or `-NpmExe <path>`), installs missing
dependencies with `npm ci`, and clears the Next.js development cache after
stopping stale workers. It respects `SPARKLE_API_BASE` from the environment and
defaults to `http://localhost:1323/api`.

Browser requests go to same-origin `/api` and `/output`; the Next.js server
proxies them to `SPARKLE_API_BASE`. Set this to the backend URL reachable from
the **frontend server/container**, including `/api`. It is a runtime setting;
changing it requires restarting the frontend process, not rebuilding the image.
Without an override, the proxy uses `http://host.docker.internal:1323/api`.
`NEXT_PUBLIC_API_BASE` is not used.

### Local machine variants

When present, `launch-backend.local.ps1` and `launch-frontend.local.ps1` are
automatically selected by their shared launchers. This includes backend launches
from the Windows tray, so existing shortcuts continue to use local settings.
The local wrappers set machine-specific values and delegate to the shared
implementation with `-NoLocalConfig`, forwarding `-BackendExecutable` for tray
launches and any executable override. For example, a backend local wrapper is:

```powershell
param([string]$BackendExecutable, [string]$GoExe = "go")
$ErrorActionPreference = "Stop"
$env:MEDIA_ROOT = "D:\Media"
$env:OUTPUT = "D:\Transcodes"
& (Join-Path $PSScriptRoot "launch-backend.ps1") -NoLocalConfig -GoExe $GoExe -BackendExecutable $BackendExecutable
```

Use `-NoLocalConfig` on either shared launcher for generic defaults or disposable
test settings. Optional `install-backend-startup.local.ps1` and
`build-windows-app.local.ps1` wrappers can pass the local Go executable to the
shared installer/builder. Local installer wrappers must forward `-Remove`,
`-NoStartup`, and `-NoStartMenu`.

Files matching `*.local.ps1`, `*.local.sh`, and `*.local.yml`, plus environment
files, are ignored by Git. Keep local paths, registry accounts, and credentials
there; they are not included in a fresh clone. The frontend Docker context also
excludes environment files and local variants.

## Backend configuration

[internal/config/config.go](../internal/config/config.go) defines all environment
variables and defaults. These are the defaults for a direct server launch.
Relative media, output, data, and cache paths are resolved against the startup
working directory before media commands change their working directory.

| Variable | Default | Purpose |
| --- | --- | --- |
| `API_ADDR` | `:1323` | API listen address. Use `127.0.0.1:1323` for loopback-only access. |
| `MEDIA_ROOT` | `./media` | Source library root. |
| `OUTPUT` | `./output` | Task directories and generated files. |
| `DATA_DIR` | `./.sparkle-transcoder` | Local application data, relative to the working directory. |
| `SCAN_CACHE_FILE` | `<DATA_DIR>/scan-cache.json` | Persisted media index; derived when unset. |
| `SCAN_INCREMENTAL` | `true` | Reuse unchanged scan data. |
| `SCAN_ON_STARTUP` | `true` | Scan the source library when the backend starts. |
| `SCAN_INPUT_INTERVAL` | `12h` | Periodic scan interval; `0` disables periodic scans. |
| `MEDIA_LIBRARIES` | `Movies,TV-Shows` | Comma-separated library directories under the root. |
| `MEDIA_EXCLUDE_DIRS` | `output,temp` | Excluded paths relative to the media root; hidden directories and the configured output directory are also skipped. |
| `FFMPEG`, `FFPROBE`, `MKVEXTRACT`, `HANDBRAKE_CLI` | `ffmpeg`, `ffprobe`, `mkvextract`, `HandBrakeCLI` | Media executable names or paths. |
| `ENCODER`, `VIDEO_EXT` | `av1`, `mp4` | Default output codec selection and extension. |
| `CONSTANT_QUALITY`, `AUDIO_KBPS` | `22`, `144` | HandBrake quality and audio bitrate defaults. |
| `SVT_AV1_ENCODER`, `AV1_PRESET` | `svt_av1_10bit`, `4` | Default AV1 implementation and preset. |
| `TASK_CONCURRENCY` | `1` | Concurrent tasks; values below 1 are clamped to 1. |
| `ENABLE_ENCODE`, `ENABLE_SPRITE`, `COPY_SUBTITLE_SIDECARS` | `true` | Default encoding, storyboards, and subtitle sidecar copying. |
| `ENABLE_LOW_PRIORITY`, `COMPUTE_SHA256`, `DEBUG` | `true`, `false`, `false` | Child process priority, source hashing, and debug logging. |

Additional HEVC/H.264 encoder settings and thumbnail dimensions/intervals are
listed in the config source. Per-task parameters override applicable defaults.
Codec names are `av1`, `hevc`, `h264-10bit`, and `h264-8bit`, plus a `-tv` variant
of each. `ENCODER` accepts a comma-separated list. Multiple codec encodes within
one task may run concurrently, independently of `TASK_CONCURRENCY`.

The API and frontend have no built-in authentication, and `:1323` binds all
interfaces. Keep them on a trusted network or behind an authenticated proxy.
Generated task files are served by the backend under `/output`.

## Scanning and task behavior

The scanner reads source media and related artwork, subtitles, and metadata.
It writes its index to `SCAN_CACHE_FILE` and excludes `OUTPUT`, configured
exclusions, and hidden directories. If no configured library directories are
available for scanning, it scans eligible immediate subdirectories of `MEDIA_ROOT`.
A forced scan bypasses incremental reuse; cache version or scan configuration
changes also trigger a full scan.

Tasks write to `OUTPUT/<task-id>/`, with metadata in `job.json`. Source files
remain unchanged. The task store can read legacy job metadata from the original
encoder. By default, new backend tasks require subtitles; disable
`requireSubtitles` for media without them. The fast path copies video and
encodes audio, so it cannot apply subtitle burn-in or TV padding.

At startup, the backend loads existing tasks and automatically requeues jobs in
`queued`, `running`, `incomplete`, or `streams_extracted` states when their input
can be found. Recovery clears that task's previous output and starts it again.
Manual retry likewise clears output and reruns the job with its saved parameters;
it does not resume a partial encode. Completed, failed, and canceled tasks are
not automatically retried.

Task-list requests use an in-memory cache. Use **Refresh tasks** or
`POST /api/tasks/refresh` to reload external changes under `OUTPUT`. Deleting a
task removes its entire output directory. Cancel an active task and wait for it
to stop before deleting it.

## API

Routes are defined in [internal/api/server.go](../internal/api/server.go).

| Method and path | Purpose |
| --- | --- |
| `GET /api/health` | Server health. |
| `GET /api/config` | Effective public configuration. |
| `GET /api/tools` | Media executable availability and versions. |
| `GET /api/scan` | Scan status and progress. |
| `POST /api/scan` | Start a scan with `{ "force": false }` (`true` for a full scan). |
| `GET /api/media` | List media; optional `q`, `kind`, and `library` filters. |
| `GET /api/media/:id` | Media details. |
| `GET /api/media/:id/poster`, `GET /api/media/:id/fanart` | Library artwork. |
| `GET /api/tasks` | Cached tasks and active status; optional `state` filter. |
| `GET /api/tasks/:id` | Task details and generated file inventory. |
| `POST /api/tasks/refresh` | Reload task metadata from output directories. |
| `POST /api/tasks` | Queue `{ "mediaId": "...", "params": { ... } }`. |
| `POST /api/tasks/:id/cancel` | Cancel queued or running work. |
| `POST /api/tasks/:id/retry` | Clear task output and queue another attempt. |
| `DELETE /api/tasks/:id` | Delete a task and its output. |
| `POST /api/tasks/delete` | Batch delete with `{ "ids": ["..."] }`; reports per-task failures. |
| `GET /output/:id/*` | Generated files. |

Task parameters are defined in [internal/task/types.go](../internal/task/types.go)
and mirrored in [web/lib/api.ts](../web/lib/api.ts).

## Windows startup and tray launcher

To build the Windows application and create Startup and Start Menu shortcuts:

```powershell
./install-backend-startup.ps1
```

The shortcuts launch `bin/windows/Sparkle.exe` directly without a terminal
window; the compiled backend is `bin/windows/Sparkle.Backend.exe`. The Start Menu
entry is named **Sparkle Transcoder**. The installer replaces the old **Sparkle**
shortcuts. To pin it to the taskbar, find Sparkle Transcoder in Start,
right-click, and select **Pin to taskbar**. The tray hosts only the backend;
start the frontend separately.

Double-click the tray icon to open live logs. Closing the logs window hides it
and leaves Sparkle Transcoder running. Launching it again opens the existing logs
window instead of another backend. The tray menu supports **Start Sparkle Transcoder**,
**Stop Sparkle Transcoder**, **Restart Sparkle Transcoder**, **Open Logs**,
**Open Log Folder**, and **Quit**. Stop leaves the tray running; Quit stops the
backend and exits.

The logs window combines standard output, standard error, and launcher messages,
with a pause control and bounded recent history. Each new tray launch clears
the previous session's display history and replaces
`.sparkle-transcoder/logs/sparkle.log` under the repository root, independently
of the backend's `DATA_DIR` setting. Reopening the logs window or restarting
the backend from the tray keeps the current session's logs.

The GUI runs `launch-backend.ps1` without a console, using its ignored local
wrapper when present, then starts the compiled backend. Shutdown first requests a graceful
stop; after 12 seconds the tray terminates remaining managed processes. Windows
job ownership also cleans up backend/encoder children if the tray crashes.

To rebuild, quit Sparkle Transcoder and run `./install-backend-startup.ps1` again
(or its local wrapper). The installer accepts `-GoExe <path>`. For a
build without installing shortcuts, use `./build-windows-app.ps1`; it accepts
`-GoExe <path>` and `-OutputDirectory <path>`. Launch with
`./bin/windows/Sparkle.exe` or `./launch-backend-tray.ps1 -ShowLogs`. Keep the
repository's launcher script available; the tray resolves the repository root
relative to `bin/windows`, or from `--repo-root <path>`.

The installer supports `-NoStartup` and `-NoStartMenu` to skip creating either
shortcut. To remove both shortcuts (without deleting binaries or task data):

```powershell
./install-backend-startup.ps1 -Remove
```

When upgrading from the older PowerShell tray launcher, quit the old instance
first. If a taskbar pin still points to the old launcher, unpin it and re-pin
Sparkle Transcoder from Start. Ensure the media drive is available when starting
the app.

## Docker frontend and publishing

The shared Compose example builds a local `sparkle-transcoder-frontend:local`
image. Set `SPARKLE_API_BASE` to the backend URL reachable from the container,
then run:

```powershell
docker compose -f docker-compose.frontend.example.yml up -d --build
```

The example maps port 3000 and provides `host.docker.internal` through
`host-gateway`. The image contains only the frontend; the backend and media
tools run separately. To use a published image instead, set `FRONTEND_IMAGE` to
your full registry image and tag, then pull and start it without a local build:

```powershell
$env:FRONTEND_IMAGE = "meinya/sparkle-transcoder-frontend:latest"
docker compose -f docker-compose.frontend.example.yml pull
docker compose -f docker-compose.frontend.example.yml up -d --no-build
```

For a local build without publishing:

```powershell
docker build -t sparkle-transcoder-frontend:local ./web
```

[Test and publish frontend image](../.github/workflows/docker.yml) runs on every
branch push, pull request, and manual dispatch. It runs Go tests with race
detection, Go vet, frontend tests, and workflow validation before building the
production image. The Docker build checks TypeScript. An isolated container
smoke test verifies non-root startup, the homepage, compiled/public assets, and
the runtime API/output proxies against a fixture backend.

With repository secrets `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN`, branch pushes
and manual runs publish the tested image under its full Git commit SHA. The
image name defaults to `meinya/sparkle-transcoder-frontend`; set the
repository variable `FRONTEND_IMAGE` to override that name (without a tag). CI
builds `linux/amd64`. Pull
requests only build and test; missing secrets skip publishing with a notice.
Only the current default-branch commit updates `latest`, with serialized
promotion to prevent an older build from replacing it. The published digest is
recorded in the run summary. Manual runs are available through
**Actions > Test and publish frontend image > Run workflow**, or
`gh workflow run docker.yml --ref main`.

[build-frontend-image.sh](../build-frontend-image.sh) is a separate manual publishing
helper requiring Bash, Docker Buildx, and registry access. It defaults to the
public image `meinya/sparkle-transcoder-frontend`; set `IMAGE_NAME` to override
the registry namespace/image (without a tag). It always pushes both
`latest` and a full commit tag (with `-dirty` appended for local changes); it does
not perform CI's tests or default-branch promotion checks. Publishing an image
does not restart existing containers.

Use `bash ./build-frontend-image.sh` directly; no local wrapper is needed.
An ignored `docker-compose.frontend.local.yml` can preserve a machine's published
image and backend URL; use it with `docker compose -f docker-compose.frontend.local.yml up -d`.

## Development and verification

See [AGENTS.md](../AGENTS.md) for contributor/agent guidance and package ownership.
Backend code is under `cmd/` and `internal/`, frontend code under `web/`, and the
Windows tray host and integration tests under `windows/`. Build products and
runtime data are ignored by Git.

Backend checks, from the repository root:

```powershell
go test ./... -count=1
go vet ./...
go test -race ./... -count=1
```

Race detection requires a working C toolchain. Root Go commands do not cover the
frontend: `web/go.mod` establishes a separate module boundary.

Frontend checks:

```powershell
Push-Location web
npm ci
npm test
npm run typecheck
npm run build
Pop-Location
```

`npm test` uses Node's test runner for library grouping/sorting behavior. There
is no npm lint script. On macOS/Linux, use `cd web` in place of PowerShell's
`Push-Location`/`Pop-Location`.

Windows tray checks:

```powershell
./windows/test-tray.ps1
./windows/test-launchers.ps1
```

The Go tests use temporary fixtures and fake media commands. The tray tests
compile an isolated fake backend under `tmp/` and exercise hidden startup,
logging, activation, lifecycle controls, and process-tree cleanup. Neither suite
needs the media library or installed encoders. Launcher tests exercise generic
defaults and local wrappers using fake commands in a disposable directory.
Windows-specific tests require
Windows and are separate from the Linux CI checks.
