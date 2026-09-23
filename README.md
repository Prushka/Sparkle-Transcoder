# Sparkle Transcoder

Standalone media scanner and transcoding task manager extracted from `Sparkle-BE/cmd/encoder`.

## Backend

```powershell
./launch-backend.ps1
```

### Windows startup and tray launcher

To run the backend automatically when you sign in and manage it from the Windows tray:

```powershell
./install-backend-startup.ps1
```

This builds the Windows application and creates:

- A Startup shortcut that launches `bin/windows/Sparkle.exe` directly on login, without a terminal window.
- A Start Menu shortcut named `Sparkle`, with the application icon embedded in the executable.
- A compiled backend, `bin/windows/Sparkle.Backend.exe`; Go is needed to build/update, not to start the installed app.

To pin a launch icon to the taskbar, open Start, search for `Sparkle`, right-click it, and choose `Pin to taskbar`.

Double-click the tray icon to open the live logs window. Closing that window hides it and leaves Sparkle running. Opening the application again brings back the same logs window instead of starting another backend. Use **Quit** in the tray context menu to exit the application and stop its backend.

The tray menu also supports `Start Sparkle`, `Stop Sparkle`, `Restart Sparkle`, `Open Logs`, and `Open Log Folder`. Stop leaves the tray running. The logs window combines standard output, standard error, and launcher messages, with a pause control for reading older output. Full logs are appended to `.sparkle-transcoder/logs/sparkle.log`; the window keeps a bounded recent history.

Backend environment settings still live in `launch-backend.ps1`. The GUI launches that script without a console to apply your settings, then runs the compiled backend. Quit first requests a graceful shutdown; after 12 seconds it stops any remaining backend/encoder processes. Windows job ownership also cleans up those processes if the tray host crashes.

To rebuild after backend or tray changes, quit Sparkle and run `./install-backend-startup.ps1` again. Building requires Go on PATH (or use `./build-windows-app.ps1 -GoExe <path>`) and the Windows .NET Framework compiler. No additional UI runtime installation is needed on Windows 10/11. You can also launch `bin/windows/Sparkle.exe` directly, or use `./launch-backend-tray.ps1 -ShowLogs` from an existing shell.

When upgrading from the older PowerShell tray launcher, quit the old tray instance before starting the new executable. If the taskbar keeps an old pinned launcher, unpin and re-pin `Sparkle` from Start.

To remove the Startup and Start Menu shortcuts:

```powershell
./install-backend-startup.ps1 -Remove
```

Default paths:

- `MEDIA_ROOT=O:\Managed-Videos`
- `OUTPUT=O:\Managed-Videos\Public\output`
- `SCAN_INCREMENTAL=true`
- `COPY_SUBTITLE_SIDECARS=true`
- `API_ADDR=:1323`

Core endpoints:

- `GET /api/media`
- `POST /api/scan` with `{ "force": false }`
- `GET /api/tasks`
- `POST /api/tasks/refresh`
- `POST /api/tasks` with `{ "mediaId": "...", "params": { ... } }`
- `POST /api/tasks/:id/cancel`
- `POST /api/tasks/:id/retry`

The scanner only reads `MEDIA_ROOT`. Task creation writes under `OUTPUT`. Existing task metadata under `OUTPUT` is reloaded only through `POST /api/tasks/refresh`.

## Frontend

```powershell
./launch-frontend.ps1
```

Set `NEXT_PUBLIC_API_BASE=http://localhost:1323/api` if the backend runs elsewhere.

## Verification

The Windows tray integration tests use an isolated fake backend and do not touch the media library:

```powershell
go test ./...
./windows/test-tray.ps1
```

```bash
/Users/dan/sdk/go1.26.3/bin/go test ./...
cd web
/opt/homebrew/bin/node ./node_modules/typescript/bin/tsc --noEmit
/opt/homebrew/bin/node ./node_modules/next/dist/bin/next build
```
