# OpenStore

Self-hosted, multi-tenant media management microservice. Any application registers with OpenStore and gets direct file uploads, security verification, quota enforcement, and webhook delivery — without touching a single file byte on its own server.

Built with Go and SeaweedFS. SeaweedFS is a private implementation detail — no external party knows it exists. The frontend, client backend, and internet see only OpenStore.

---

## Why OpenStore Exists

Most applications implement file upload logic in-house: validate on the server, stream bytes through the app, save to disk or S3, run checks, update the database. This is expensive — the app server is in the critical path for every byte uploaded.

OpenStore removes the app server from that path entirely. The frontend uploads directly to OpenStore. OpenStore handles the full security layer — MIME validation, magic byte verification, antivirus scanning, quota enforcement — and delivers a webhook when done. The client backend saves the media ID and URL. That is all it does.

---

## Requirements

- Go 1.24 or later (to build from source)
- Docker and Docker Compose

SeaweedFS is used instead of MinIO because MinIO ceased development in April 2026 and receives no security updates. SeaweedFS is Apache 2.0 licensed and actively maintained.

---

## Quick Start

Clone the repository:

```bash
git clone https://github.com/you/openstore
cd openstore
```

Install dependencies and generate `go.sum`:

```bash
make tidy
```

Generate an API key:

```bash
make key
# outputs: ops_live_a1b2c3d4e5f6...
```

Create your `.env` file:

```bash
cp .env.example .env
```

Fill in the four required values:

```bash
OPENSTORE_API_KEY=ops_live_...        # from make key
OPENSTORE_SEAWEEDFS_FILER_ADDR=seaweedfs:18888   # default, no change needed
OPENSTORE_DB_PATH=/app/data/openstore.db          # default, no change needed
OPENSTORE_CLAMAV_URL=tcp://clamav:3310            # default, no change needed
```

Build and start all services:

```bash
make build
make up
```

This starts SeaweedFS, ClamAV, and OpenStore. ClamAV takes up to 2 minutes to load its virus database on first boot. OpenStore waits for both services to be healthy before starting.

Verify everything is running:

```bash
make health-deep
```

---

## Building from Source

```bash
make bin
```

Produces a single static binary `./openstore`. No runtime dependencies beyond the SQLite file and access to SeaweedFS.

---

## Environment Variables

The `.env` file requires exactly four values. Everything else has a hardcoded default that works correctly without configuration.

```bash
# Generate with: make key
OPENSTORE_API_KEY=ops_live_your_key_here

# SeaweedFS Filer gRPC address — internal Docker network, no http:// prefix
OPENSTORE_SEAWEEDFS_FILER_ADDR=seaweedfs:18888

# SQLite database path — must point to a mounted Docker volume
OPENSTORE_DB_PATH=/app/data/openstore.db

# ClamAV daemon — internal Docker network
OPENSTORE_CLAMAV_URL=tcp://clamav:3310
```

Optional tuning (only set these if you have a reason to deviate from defaults):

```bash
OPENSTORE_LOG_LEVEL=info             # debug / info / warn / error
OPENSTORE_PRESIGN_TTL_DEFAULT=300    # upload token TTL in seconds
OPENSTORE_PRESIGN_TTL_MAX=86400      # maximum allowed TTL
OPENSTORE_READ_TTL_DEFAULT=900       # read token TTL for private buckets
OPENSTORE_CLAMAV_ENABLED=true        # set false only in local dev
```

---

## Pre-Configuration

Before uploads can happen, configure OpenStore with your project and buckets in a single call. Every request requires the API key in the Authorization header.

```bash
curl -X POST http://localhost:8080/configure \
  -H "Authorization: Bearer <OPENSTORE_API_KEY>" \
  -H "Content-Type: application/json" \
  -d '{
    "project": {
      "name": "mediavault",
      "webhook_url": "http://localhost:8000/webhooks/openstore",
      "webhook_secret": "strong-random-value",
      "allowed_origins": ["http://localhost:3000"],
      "quota_bytes": 10737418240
    },
    "buckets": [
      {
        "name": "mediavault-avatars",
        "media_class": "images",
        "allowed_mime": ["image/jpeg", "image/png", "image/webp"],
        "max_bytes": 5242880,
        "presign_ttl_seconds": 300,
        "access": "public"
      },
      {
        "name": "mediavault-contracts",
        "media_class": "documents",
        "allowed_mime": ["application/pdf"],
        "max_bytes": 52428800,
        "presign_ttl_seconds": 300,
        "read_ttl_seconds": 600,
        "access": "private"
      }
    ]
  }'
```

If any bucket fails validation the entire request rolls back — nothing is created partially.

Verify configuration at any time:

```bash
curl http://localhost:8080/configure \
  -H "Authorization: Bearer <OPENSTORE_API_KEY>"
```

---

## Upload Flow

1. Client backend calls `POST /upload/presign` with the API key
2. OpenStore returns a signed upload URL and `upload_id`
3. Client backend passes both to the frontend
4. Frontend PUTs the file directly to OpenStore using the signed URL
5. OpenStore streams bytes to SeaweedFS via gRPC, runs full verification inline
6. OpenStore returns the result to the frontend
7. OpenStore fires a webhook to the client backend
8. Client backend receives the webhook and saves the media ID and URL

The user is never blocked waiting for verification. The browser sees one upload operation. The webhook arrives at the client backend as the upload completes.

---

## Bucket Access Policy

**Public buckets** — OpenStore returns a permanent URL after verification. The frontend reads files via `GET /files/{upload_id}` on OpenStore, which proxies from SeaweedFS.

**Private buckets** — no permanent URL is generated. The frontend reads files via `GET /files/{upload_id}` with an authorisation token. The client backend controls who gets to request reads.

SeaweedFS is never directly reachable from the frontend. All reads go through OpenStore.

---

## Migrations

OpenStore runs migrations automatically on startup. To run them manually:

```bash
make migrate-up        # apply all pending
make migrate-down      # roll back (prompts for step count)
make migrate-version   # print current version
make migrate-force     # force version (for dirty state recovery)
```

Never edit an already-applied migration. Write a new numbered migration file for every schema change.

---

## Docker Compose

```yaml
services:
  seaweedfs:
    image: chrislusf/seaweedfs:4.40
    expose: ["9333", "8888", "18888"]   # internal only

  clamav:
    image: clamav/clamav:1.5
    expose: ["3310"]                    # internal only

  openstore:
    build: .
    ports:
      - "8080:8080"                     # only public port
```

SeaweedFS and ClamAV are never reachable from outside Docker. Only OpenStore port 8080 is public.

---

## Production Deployment

Put a reverse proxy in front of OpenStore with TLS:

```
api.example.com  →  OpenStore :8080
```

SeaweedFS stays entirely internal. It never needs a public address because the frontend uploads to OpenStore, not SeaweedFS directly.

```bash
# Caddy example
api.example.com {
    reverse_proxy openstore:8080
}
```

---

## Project Structure

```
cmd/openstore/main.go           Entry point
scripts/keygen/main.go          API key generation
scripts/migrate/main.go         Manual migration control
internal/config/                Environment variable parsing
internal/db/                    SQLite connection and migrations
internal/db/migrations/         Numbered up/down SQL files
internal/models/                Structs and database methods
internal/handlers/configure.go  POST/GET/PUT/PATCH/DELETE /configure
internal/handlers/upload.go     POST /upload/presign, PUT /upload/{id}
internal/handlers/files.go      GET /files/{id}, DELETE /files/{id}
internal/handlers/health.go     GET /health, GET /health/deep
internal/middleware/            Auth, logging, panic recovery
internal/seaweedfs/             SeaweedFS Filer gRPC client
internal/security/              Magic bytes, MIME, token signing
internal/webhook/               Delivery and retry
internal/quota/                 Quota check and deduction
```

---

## Running Tests

```bash
make test          # run all tests
make test-cover    # coverage report
```

Tests use an in-memory SQLite database and a mock SeaweedFS gRPC server. No external dependencies required.
