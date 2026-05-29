# Sparkle Transcoder

Standalone media scanner and transcoding task manager extracted from `Sparkle-BE/cmd/encoder`.

## Backend

```powershell
./launch-backend.ps1
```

Default paths:

- `MEDIA_ROOT=O:\Managed-Videos`
- `OUTPUT=O:\Managed-Videos\Public\output`
- `SCAN_INCREMENTAL=true`
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
