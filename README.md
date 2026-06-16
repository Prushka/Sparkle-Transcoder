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

This creates:

- A Startup shortcut that runs `launch-backend-tray.ps1` on login.
- A Start Menu shortcut named `Sparkle` using the project icon from `assets/sparkle-transcoder.ico`.

To pin a launch icon to the taskbar, open Start, search for `Sparkle`, right-click it, and choose `Pin to taskbar`.

The tray menu supports `Start Sparkle`, `Stop Sparkle`, `Restart Sparkle`, `Open Logs`, and `Quit`. Logs are written under `.sparkle-transcoder/logs`.

Run `./install-backend-startup.ps1` again to refresh existing shortcuts after the icon or launcher changes. If the taskbar keeps an old name or icon, unpin and re-pin `Sparkle` from Start.

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

```bash
/Users/dan/sdk/go1.26.3/bin/go test ./...
cd web
/opt/homebrew/bin/node ./node_modules/typescript/bin/tsc --noEmit
/opt/homebrew/bin/node ./node_modules/next/dist/bin/next build
```
