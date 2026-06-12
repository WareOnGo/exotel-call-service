# exotel-call-service — Exotel call-management microservice

Go (Gin + GORM) service that sits between Exotel and Supabase:

- **Routes incoming calls** to the right POC (sticky to whoever the caller last
  spoke to; first-touch POC + fallback for new callers).
- **Syncs call history** into Supabase — real-time via StatusCallback webhook,
  with a periodic reconcile backstop.
- **Exposes click-to-call** (Exotel "connect two numbers").

## Architecture

```
                 ┌─────────────────────────── Exotel ───────────────────────────┐
incoming call ──▶│ ExoPhone flow ▶ Connect applet (dynamic URL) ─────┐           │
                 │ call completes ▶ StatusCallback webhook ───────┐  │           │
                 └────────────────────────────────────────────────┼──┼──────────┘
                                                                   │  │
                                          POST /webhooks/call-status│  │GET/POST /route
                                                                   ▼  ▼
                                                          ┌──────────────────┐
   cron / ticker ── reconcile (fill gaps) ───────────────▶│     exotel-call-service      │
   POST /api/calls/connect (click-to-call) ──────────────▶│  (Gin + GORM)    │
                                                          └────────┬─────────┘
                                                                   │ pgx pool
                                                                   ▼
                                                            Supabase (Postgres)
```

### Why this shape
- **Router does one indexed read** (`assignments.customer_phone`), with a hard
  timeout → static fallback so a slow DB never causes dead air. The assignment
  write happens *after* the response, off the latency path.
- **Webhook = real-time** ingestion; **reconcile = backstop** for missed events
  (idempotent upsert on `exotel_sid`).

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| GET | `/healthz` | liveness + DB ping |
| GET/POST | `/route` | **Connect applet dynamic URL** — returns number(s) to dial |
| POST | `/webhooks/call-status` | StatusCallback ingestion (idempotent) |
| POST | `/sync` | manual reconcile (guard: `X-Sync-Secret`) |
| POST | `/archive-recordings` | copy un-archived recordings to R2 (guard: `X-Sync-Secret`) |
| POST | `/backfill-assignments` | seed sticky assignments from existing calls — outbound (POC=From) and inbound (POC=To) (guard: `X-Sync-Secret`) |
| POST | `/api/calls/connect` | click-to-call: `{from,to,caller_id}` |
| GET | `/api/calls` | recent calls (`?limit&offset&status&from`) |
| GET/POST | `/api/contacts` | list / create POCs |
| PATCH | `/api/contacts/:id` | update a POC |
| GET | `/api/assignments` | list customer↔POC bindings |
| PUT | `/api/assignments` | manually (re)assign `{customer_phone,contact_id}` |

## Run locally

```bash
cp .env.example .env      # fill DATABASE_URL + Exotel creds
go mod tidy
go run ./cmd/server
```

Tables are auto-migrated on boot. Seed a first-touch POC:

```bash
curl -X POST localhost:8080/api/contacts -H 'content-type: application/json' \
  -d '{"name":"Sales POC","phone":"080XXXXXXXX","is_first_touch":true,"fallback_priority":0,"active":true}'
```

## Testing

```bash
make test        # hermetic: in-memory SQLite + mock Exotel (httptest)
make test-race   # with the race detector
make test-pg     # against a real Postgres (set TEST_DATABASE_URL)
make cover       # coverage summary
```

Tests are hermetic by default — no Postgres or network needed (SQLite in-memory +
`httptest` mocks for Exotel + a fake `ObjectStore` for R2). The exact same suite
runs against real Postgres when `TEST_DATABASE_URL` is set; CI runs both tiers
(`.github/workflows/ci.yml`). Coverage spans every endpoint, the router decision
matrix, webhook idempotency, reconcile enrichment, and the archive worker
(success / download-fail / store-fail / max-attempts / skip-already-archived).

## Deploy

Single static binary; `Dockerfile` included. Run as a **long-running container**
(Fly.io / Railway / Render / VM) in the **same region as your Supabase project**,
using the **direct** connection string (port 5432). The reconciler runs as an
in-process ticker — no external scheduler needed (though `POST /sync` is there if
you want one).

## ⚠️ Open integration item — the `/route` response format

Exotel gates the exact request params + response contract for the Connect
applet's dynamic "Application URL" behind support (hello@exotel.com). The
`/route` handler currently returns a **comma-separated list of numbers as
text/plain**, which is the common expectation — but **confirm it against a real
flow before going live**. The format lives in one place:
`internal/handlers/route.go → respondDial()`. Change only that function.

The Passthru applet (binary `200`/`302` branching) is *not* used here because it
can't return a number to dial.

## Recording archive (Cloudflare R2)

To avoid vendor lock-in, recordings are copied from Exotel into your own R2
bucket. The archiver is **disabled until the `R2_*` env vars are set** (the
endpoint then 503s and the ticker doesn't start).

- Idempotent + resumable: each run selects `recording_url <> '' AND
  recording_r2_key IS NULL`, oldest first (closest to Exotel's ~6-month expiry),
  skipping rows past `ARCHIVE_MAX_ATTEMPTS`.
- For each: download from Exotel (Basic auth, same key/token) → `PutObject` to R2
  → write `recording_r2_key` + `recording_archived_at`. Failures bump
  `recording_archive_attempts` and store `recording_archive_error`.
- We store the **object key** (`recordings/<sid>.mp3`), not a URL — generate URLs
  from a public bucket / custom domain so the storage domain stays swappable.
- Runs as a nightly in-process ticker (`ARCHIVE_INTERVAL_HOURS`) and via
  `POST /archive-recordings` for an external cron.
- `ObjectStore` is an interface (`internal/archive/r2.go`) — swap R2 for any
  S3-compatible target without touching the archive logic.

Set `R2_ACCOUNT_ID` (or `R2_ENDPOINT`), `R2_ACCESS_KEY_ID`,
`R2_SECRET_ACCESS_KEY`, `R2_BUCKET` to enable.

## Security notes

- `/route` and `/webhooks/call-status` are public (Exotel calls them). Restrict
  to Exotel's published IP ranges at your proxy/LB (see the `exotel-api` skill →
  `advanced-config/ip-whitelisting`).
- Guard `/sync` with `SYNC_SECRET`. Keep `/api/*` behind your own auth / private
  network — it's admin surface.
- Phone matching normalizes to the last 10 digits (Indian numbers); swap in
  libphonenumber for multi-country support (`internal/util/phone.go`).
