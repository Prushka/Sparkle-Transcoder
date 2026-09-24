# Sparkle Transcoder Agent Guide

This guide applies throughout the repository. More specific instructions in a
nested `AGENTS.md` apply to that subtree. Treat current code and tests as the
source of truth; use [README.md](README.md) for setup and operation.

Update this guide when a durable project fact changes, and update README.md when
setup, configuration, or user-visible behavior changes. Replace stale guidance
instead of appending a history of work. Keep transient logs, process IDs, and
validation transcripts out of this file.

## Project and ownership

Sparkle Transcoder is a standalone media scanner and transcoding task manager
extracted from `Sparkle-BE/cmd/encoder`. It has a Go HTTP backend, a Next.js
frontend, and an optional Windows Forms tray host.

- `cmd/server/main.go`: startup, periodic scanning, task recovery, and shutdown.
- `internal/config/config.go`: backend environment variables and defaults.
- `internal/api/server.go`: Echo routes and JSON responses.
- `internal/media/`: library scanning, names, related files, and scan cache.
- `internal/task/`: persisted jobs, queue/retry/recovery, and encoder pipeline.
- `internal/executil/`, `internal/lifecycle/`, `internal/priority/`: child process
  execution and platform-specific lifecycle behavior.
- `web/components/dashboard.tsx`: dashboard; `web/lib/library.ts`: library
  grouping and sorting; `web/lib/api.ts`: browser API client and response types.
- `web/lib/backend-proxy.ts` and `web/app/`: runtime API/output proxy and pages.
- `windows/Sparkle.cs` and root PowerShell scripts: tray host, builds, launch,
  and shortcut installation. There is no .NET SDK project; scripts use the
  Windows .NET Framework C# compiler. The Windows display and shortcut name is
  **Sparkle Transcoder**; the executable remains `bin/windows/Sparkle.exe`.
- `.github/workflows/docker.yml`: Linux checks and frontend image publishing.

The root Go module is `sparkle-transcoder` and requires Go 1.26.0 or newer.
`web/go.mod` is a separate module boundary; root `go test ./...` does not verify
the frontend. Use npm with `web/package-lock.json`; Node 22 matches CI and the
Docker image (the declared minimum is 20.9.0).

## Contracts to preserve

- Source media and sidecars are read-only inputs. Scanning writes its cache to
  `SCAN_CACHE_FILE`; task artifacts and `job.json` belong under `OUTPUT/<id>`.
  Keep output and excluded directories out of the scan. Preserve cache
  invalidation when the root, library selection, exclusions, or schema changes.
- `Store.List` serves cached task summaries. Disk refresh happens at startup or
  through `POST /api/tasks/refresh`; polling must not scan the output tree.
  Preserve active task updates when a concurrent refresh finishes.
- Startup recovers `queued`, `running`, `incomplete`, and `streams_extracted`
  jobs. Recovery and retry clear the task's output directory and run it again;
  they do not resume a partial encoder file. Preserve input validation, task-ID
  and output-containment checks, and rejection of retry/delete while active.
- Keep legacy `job.json` decoding and optional boolean defaults compatible.
  Change Go JSON contracts and `web/lib/api.ts` together. Use the shared task
  persistence helpers instead of adding independent writes to `job.json`.
- Run media commands through `executil.Runner` with cancellation. Preserve
  `runWithMediaSource`/`mediaSourceArg`, which run source-consuming tools in the
  input directory with the source basename. Keep Windows children hidden and
  process output bounded; maintain implementations for other supported OSes.
- `-tv` encoder variants add subtitle burn-in and 16:9 padding. They require
  encoding and are incompatible with the fast copy path. Preserve distinct
  output names when standard and TV variants are requested together.
- Browser requests use same-origin `/api` and `/output`. `SPARKLE_API_BASE` is
  read by the Next.js server at runtime; do not replace it with a browser URL or
  a build-time public environment variable.
- The tray owns its backend/encoder process tree. Preserve the startup gate,
  shutdown event, Windows job cleanup, single-instance activation, and the
  distinction between closing the logs window, stopping the backend, and Quit.
- Each new tray session starts with an empty log buffer and replaces
  `.sparkle-transcoder/logs/sparkle.log`. Reopening the logs window, activating
  the existing tray, or restarting its backend preserves that session's logs.

## Working locally

Tracked launch scripts use PATH and generic defaults. Machine settings belong
in ignored `*.local.ps1`, `*.local.sh`, `*.local.yml`, or environment files.
Shared backend/frontend launchers automatically invoke their local wrappers;
wrappers must forward arguments and pass `-NoLocalConfig` when calling back into
the shared implementation. The tray relies on `-BackendExecutable` being
forwarded. Use `-NoLocalConfig` or explicit environment variables with
`go run ./cmd/server` when testing against another location. Starting the real
backend can scan the library and restart saved jobs even without an API request.
For smoke tests, use disposable `MEDIA_ROOT`, `OUTPUT`, and `DATA_DIR`, an unused
API port, `SCAN_ON_STARTUP=false`, and `SCAN_INPUT_INTERVAL=0`.

The existing Go/frontend tests use local fixtures and fake media commands; they
do not need the live library or installed encoders. The Windows tray suite uses
an isolated fake backend and its own child processes. Run relevant checks and
fix failures caused by the requested change without an extra approval step.
Live encodes, app restarts, shortcut installation, and publishing should be part
of the requested work, not incidental validation.

Keep generated/runtime files out of commits: `bin/`, `tmp/`,
`.sparkle-transcoder/`, `web/node_modules/`, `web/.next/`, `web/out/`,
`media/`, `output/`, `web/next-env.d.ts`, and `web/tsconfig.tsbuildinfo`. Preserve
local wrappers; do not force-add ignored machine settings. Format changed Go files with
`gofmt`; follow the existing TypeScript and Windows Framework C# conventions.

## Verification

Choose checks for the changed area. Documentation-only edits need path, command,
and behavior checks against source, plus `git diff --check`.

```powershell
# Backend, from the repository root.
go test ./... -count=1
go vet ./...
# Queue, cancellation, or shared-state changes; requires a C toolchain.
go test -race ./... -count=1

# Frontend; npm ci is needed on a fresh checkout or after lockfile changes.
Push-Location web
npm ci
npm test
npm run typecheck
npm run build
Pop-Location

# Windows tray/process/launcher changes, from the repository root.
./windows/test-tray.ps1
./windows/test-launchers.ps1
```

Use focused Go package/tests while iterating. Frontend tests cover library logic;
check affected dashboard interactions for UI changes. There is no npm lint
script. Windows-specific tests need Windows; Linux CI does not validate the tray.
Use `build-windows-app.ps1 -OutputDirectory <scratch-directory>` for an isolated
build, or quit the installed tray before replacing `bin/windows` binaries.

For workflow edits, run
`go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12`. For Docker/proxy work,
follow the isolated container smoke test in `.github/workflows/docker.yml` and
`.github/scripts/smoke-test-frontend.mjs`. `build-frontend-image.sh` always pushes
both `latest` and a commit tag; use a local `docker build` for build-only checks.
The helper and CI default to the public image `meinya/sparkle-transcoder-frontend`.
Override it with `IMAGE_NAME` in the helper or the `FRONTEND_IMAGE` repository
variable in CI. The helper needs no local wrapper. Keep registry credentials
out of tracked configuration.
Report checks actually run and any platform or tool limitations.

## Guide maintenance references

Keep instructions concise and specific to this repository, with details beside
their owning code. See [AGENTS.md discovery and scope](https://learn.chatgpt.com/docs/agent-configuration/agents-md)
and [OpenAI guidance on maintaining instructions](https://developers.openai.com/blog/rethinking-skills-and-prompts-for-gpt-6-astra).
