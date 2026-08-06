# OpenStore — Architecture

## What OpenStore Is

OpenStore is a self-hosted, multi-tenant media management microservice written in Go. It sits between any client application and SeaweedFS, owning the full lifecycle of a file upload: receiving file bytes directly from the browser, security verification, quota enforcement, webhook delivery, and deletion.

Any number of independent client projects can register with a single OpenStore instance. Each project gets isolated buckets, its own quota, its own allowed media rules, its own access policy per bucket, and its own webhook endpoint. OpenStore knows nothing about the client's users or business logic. It operates purely at the file and project level.

SeaweedFS is a private implementation detail. No external party knows it exists. The frontend, the client backend, and the internet see only OpenStore. SeaweedFS is reachable exclusively from OpenStore over an internal Docker network.

The client backend's only responsibilities are: requesting presigned upload tokens from OpenStore, returning those tokens to the frontend, receiving the webhook after verification, and saving the returned media ID and URL. All security logic — MIME validation, magic byte verification, size enforcement, quota management, antivirus scanning — lives entirely in OpenStore.

---

## Why These Technology Choices

**Go** — compiles to a single static binary with no runtime dependencies. Goroutines handle thousands of concurrent verifications cheaply. Ideal for IO-bound work like streaming file bytes and gRPC calls.

**SeaweedFS** — Apache 2.0 licensed, actively maintained, handles billions of small files efficiently with O(1) disk access. MinIO's open source community edition ceased development in April 2026 and receives no security patches. SeaweedFS is the correct replacement.

**SeaweedFS transport — HTTP + gRPC split** — OpenStore uses two SeaweedFS transports, each for what it is designed for:

- **Filer HTTP API on port 8888** — data operations: writing and reading file bytes. A `PUT /path` to the Filer handles volume assignment, chunking, replication, and metadata commit in a single call. A `GET /path` streams bytes back. This is SeaweedFS's first-class write and read path — it is what the SeaweedFS S3 gateway and `weed filer.copy` use internally.
- **Filer gRPC API on port 18888** — metadata operations: `DeleteEntry`, `LookupDirectoryEntry`, and the `Ping` used by the deep health check. These are pure metadata RPCs that gRPC handles correctly and efficiently.

Both ports use `expose` in docker-compose — container-to-container only, never reachable from outside Docker. The SeaweedFS S3 API is not used.

**SQLite with WAL mode** — OpenStore is a single binary on a single machine. SQLite in WAL mode supports thousands of concurrent readers with non-blocking writes. Zero network overhead, zero operational burden. The entire database is one file. Postgres is only warranted when running multiple OpenStore instances behind a load balancer, which is out of scope for this design.

**Chi router** — lightweight HTTP router for Go with zero allocations on the hot path. No heavy framework. The standard library handles everything else.

**golang-migrate** — migration library that manages schema changes as versioned SQL files embedded directly in the binary. No external migration tooling required at runtime.

**No Redis, no message queue** — webhook delivery runs in a goroutine with its own retry loop. State lives only in SQLite. Fewer moving parts means fewer failure modes. The client backend is released immediately after upload verification completes. Webhook delivery completes asynchronously.

---

## System Diagram

```
Client Backend
     |
     | 1. POST /upload/presign  (with API key)
     v
OpenStore (Go)
     |
     | 2. Validates project, bucket, mime, size, quota headroom
     | 3. Generates signed upload token (HMAC-SHA256, embeds upload_id, expiry)
     | 4. Creates upload record (status: pending)
     | 5. Returns signed upload URL pointing to OpenStore + upload_id
     v
Client Backend
     |
     | 6. Returns signed upload URL + upload_id to frontend
     v
Client Frontend (Browser)
     |
     | 7. PUT /upload/{upload_id}?token=<signed_token>
     |    File bytes stream directly to OpenStore
     |    Browser tracks progress natively — user is not blocked
     v
OpenStore (Go)
     |
     | 8.  Validates signed token (expiry, HMAC signature, upload_id match)
     | 9.  Streams file bytes to SeaweedFS via Filer HTTP PUT (no buffering)
     | 10. Magic bytes check on first 12 bytes during stream
     | 11. ClamAV antivirus scan
     | 12. Deep content inspection per media class
     | 13. Deduct quota
     | 14. Mark upload verified, store URL or object key
     | 15. Return 200 to frontend
     | 16. Fire webhook to client backend (goroutine, non-blocking)
     v
Client Backend
     |
     | 17. Receives webhook, saves media ID and URL to own database
     |
     | 18. For private bucket reads: GET /files/{upload_id}
     |     OpenStore fetches from SeaweedFS via Filer HTTP GET, streams to frontend
```

---

## Authentication

OpenStore uses a single shared API key model.

**Key generation** — a utility script at `scripts/keygen/main.go` generates a cryptographically secure random key prefixed with `ops_live_`. This key is copied into both the OpenStore `.env` and the client backend `.env` as `OPENSTORE_API_KEY`.

**Request authentication** — every request from the client backend to OpenStore must include the key in the `Authorization` header:

```
Authorization: Bearer <OPENSTORE_API_KEY>
```

OpenStore compares the incoming key against its own copy using constant-time equality to prevent timing attacks. TLS encrypts the key in transit.

**Upload token authentication** — the frontend authenticates its PUT request using a signed upload token embedded in the URL query string, not the API key. OpenStore generates this token using HMAC-SHA256 signed with the API key. The token embeds the upload ID, bucket, declared MIME type, declared file size, and expiry timestamp. OpenStore validates the token signature and all embedded claims when the PUT arrives. The API key never reaches the browser.

**Webhook signature** — when OpenStore calls the client backend's webhook URL, it includes an `X-OpenStore-Signature` header containing an HMAC-SHA256 of the request body signed with the project's `webhook_secret`.

**SeaweedFS communication** — OpenStore connects to SeaweedFS over the internal Docker network using two ports, both exposed only to containers on that network. No credentials are required because neither port is reachable from outside Docker. Network isolation is the security boundary.

---

## Pre-Configuration

Before any upload can happen, the client backend must configure OpenStore with the project and all required buckets. This is done in a single atomic call to `POST /configure`.

The configuration is declarative. On first deployment the client backend calls `POST /configure` to create everything. On subsequent deployments it calls `GET /configure` to verify the current state matches what it expects. If there is drift, it calls `PUT /configure` to reconcile.

`POST /configure` creates the project and all buckets in a single SQLite transaction. If any bucket fails validation, the entire operation rolls back.

`PUT /configure` reconciles the full configuration. It updates project fields, adds new buckets, and updates existing buckets. It does not delete buckets that are present in the database but absent from the request.

`PATCH /configure/buckets/:bucket_name` allows targeted edits to a single bucket without resending the full configuration.

---

## Bucket Design

A bucket is the primary policy container. Multiple buckets of the same media class are fully supported. The media class only controls which magic byte signatures are applied during verification.

```
mediavault-avatars          images    public    5 MB    300s TTL
mediavault-post-images      images    public    20 MB   300s TTL
mediavault-private-proofs   images    private   50 MB   300s TTL
mediavault-full-videos      videos    private   5 GB    3600s TTL
mediavault-contracts        documents private   10 MB   300s TTL
```

---

## Bucket Access Policy

Each bucket is configured as either `public` or `private` at creation time.

**Public buckets** — after verification, OpenStore constructs a permanent public URL and stores it on the upload record. The webhook payload includes this URL. The frontend reads files by calling `GET /files/{upload_id}` on OpenStore, which fetches from SeaweedFS via Filer HTTP GET and streams the bytes back.

**Private buckets** — after verification, only the object key is stored. `public_url` is null in the webhook payload. The frontend reads files the same way — `GET /files/{upload_id}` — but OpenStore enforces that the request is authorised before streaming. The client backend controls access by only giving the frontend URLs for files the user is permitted to read.

SeaweedFS is never directly reachable from the frontend. All reads go through OpenStore.

---

## Presigned Upload Tokens

When the client backend calls `POST /upload/presign`, OpenStore generates a signed upload token and returns a URL pointing to its own upload endpoint:

```
PUT /upload/{upload_id}?token=<hmac_signed_token>
```

The token is an HMAC-SHA256 signature over a payload containing:

- `upload_id`
- `bucket_name`
- `mime_type`
- `file_size`
- `expires_at` (Unix timestamp)

Signed with the API key. When the frontend PUTs to the upload endpoint, OpenStore validates the signature, checks expiry, and verifies all claims match the upload record before accepting any bytes.

TTL resolution follows three layers: per-request override → per-bucket default → global default. A hard ceiling `OPENSTORE_PRESIGN_TTL_MAX` applies regardless.

---

## Upload Flow and Streaming

When the frontend PUTs to `PUT /upload/{upload_id}?token=<token>`, OpenStore:

1. Validates the token signature and claims
2. Opens a streaming HTTP PUT to the SeaweedFS Filer on port 8888
3. Pipes `r.Body` directly into the PUT body — no buffering, constant memory regardless of file size
4. Inspects the first 12 bytes in-stream for magic byte verification
5. After stream completes, triggers ClamAV scan and deep content inspection
6. On pass — marks upload verified, fires webhook
7. On fail — calls DELETE on the Filer HTTP API to remove the object, marks upload rejected, fires webhook

The frontend receives the response only after all verification passes or fails. The user sees a single upload operation complete. The webhook arrives at the client backend shortly after.

---

## Security Verification Chain

**Step 1 — Token validation:** signature, expiry, all embedded claims.

**Step 2 — Ownership:** upload record must belong to the authenticated project.

**Step 3 — Status gate:** upload must be in `pending` state. Prevents replay.

**Step 4 — Size enforcement:** bytes received must not exceed bucket `max_bytes` or project quota. Enforced during streaming — upload is aborted mid-stream if exceeded.

**Step 5 — MIME re-check:** Content-Type header on the PUT must match the declared type and be in the bucket's `allowed_mime` list.

**Step 6 — Magic bytes:** first 12 bytes inspected against known signatures for the declared MIME type. MP4 checked at offset 4. WebP checked at both offset 0 (RIFF) and offset 8 (WEBP).

**Step 7 — ClamAV scan:** full file streamed to ClamAV daemon. Fails closed — if ClamAV is unreachable the upload is rejected.

**Step 8 — Deep content inspection:** images re-encoded via libvips stripping all metadata. PDFs parsed for embedded JavaScript, executables, and launch actions via pdfcpu.

**Step 9 — Quota deduction:** `used_bytes` incremented inside a SQLite transaction.

**Step 10 — Record update and webhook.**

---

## Magic Byte Signatures

```
image/jpeg       FF D8 FF                          (offset 0)
image/png        89 50 4E 47 0D 0A 1A 0A           (offset 0)
image/webp       52 49 46 46 at 0 + 57 45 42 50 at 8
image/gif        47 49 46 38                        (offset 0)
video/mp4        66 74 79 70                        (offset 4)
video/webm       1A 45 DF A3                        (offset 0)
audio/mpeg       49 44 33 / FF FB / FF F3 / FF F2   (offset 0)
audio/wav        52 49 46 46                        (offset 0)
audio/ogg        4F 67 67 53                        (offset 0)
audio/flac       66 4C 61 43                        (offset 0)
application/pdf  25 50 44 46                        (offset 0)
```

---

## Webhook Payload

On verification of a public bucket upload:

```json
{
  "event": "upload.verified",
  "upload_id": "01J...",
  "project_id": "01J...",
  "object_key": "images/01J.../2026/07/01J....jpg",
  "bucket": "mediavault-avatars",
  "access": "public",
  "public_url": "https://yourdomain.com/files/01J...",
  "content_type": "image/jpeg",
  "size_bytes": 204800,
  "verified_at": "2026-07-01T14:22:01Z"
}
```

On verification of a private bucket upload:

```json
{
  "event": "upload.verified",
  "upload_id": "01J...",
  "project_id": "01J...",
  "object_key": "documents/01J.../2026/07/01J....pdf",
  "bucket": "mediavault-contracts",
  "access": "private",
  "public_url": null,
  "content_type": "application/pdf",
  "size_bytes": 512000,
  "verified_at": "2026-07-01T14:22:01Z"
}
```

On rejection:

```json
{
  "event": "upload.rejected",
  "upload_id": "01J...",
  "project_id": "01J...",
  "rejection_reason": "magic bytes do not match declared MIME type image/jpeg",
  "rejected_at": "2026-07-01T14:22:01Z"
}
```

---

## Webhook Retry Logic

- Attempt 1 — immediate
- Attempt 2 — 10 seconds
- Attempt 3 — 30 seconds
- Attempt 4 — 2 minutes
- Attempt 5 — 10 minutes

After 5 failed attempts the delivery is marked permanently failed. All attempts recorded in `webhook_deliveries`.

---

## Quota Enforcement

Quota is enforced at two points. At presign time, OpenStore checks that the declared file size does not exceed `quota_bytes - used_bytes`. During upload streaming, OpenStore aborts the stream if bytes received exceed the quota. Both checks use the same SQLite transaction model to prevent race conditions under concurrent uploads.

---

## Migrations

OpenStore uses golang-migrate. All migration files are plain SQL embedded into the binary using Go's `embed` package.

```
internal/db/migrations/
  000001_create_projects.up.sql
  000001_create_projects.down.sql
  000002_create_buckets.up.sql
  000002_create_buckets.down.sql
  000003_create_uploads.up.sql
  000003_create_uploads.down.sql
  000004_create_webhook_deliveries.up.sql
  000004_create_webhook_deliveries.down.sql
```

On startup `m.Up()` applies any pending migrations and skips ones already applied. Never edit an already-applied migration — write a new numbered file. The migration history is append-only.

---

## Database Schema

### projects

```sql
CREATE TABLE projects (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    api_key_hash    TEXT NOT NULL,
    webhook_url     TEXT NOT NULL,
    webhook_secret  TEXT NOT NULL,
    allowed_origins TEXT NOT NULL,
    quota_bytes     INTEGER NOT NULL DEFAULT 0,
    used_bytes      INTEGER NOT NULL DEFAULT 0,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

### buckets

```sql
CREATE TABLE buckets (
    id                  TEXT PRIMARY KEY,
    project_id          TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name                TEXT NOT NULL,
    media_class         TEXT NOT NULL,
    allowed_mime        TEXT NOT NULL,
    max_bytes           INTEGER NOT NULL,
    presign_ttl_seconds INTEGER NOT NULL DEFAULT 300,
    read_ttl_seconds    INTEGER NOT NULL DEFAULT 900,
    access              TEXT NOT NULL DEFAULT 'public',
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(project_id, name)
);
```

### uploads

```sql
CREATE TABLE uploads (
    id               TEXT PRIMARY KEY,
    project_id       TEXT NOT NULL REFERENCES projects(id),
    bucket_id        TEXT NOT NULL REFERENCES buckets(id),
    object_key       TEXT NOT NULL,
    original_name    TEXT NOT NULL,
    content_type     TEXT NOT NULL,
    size_bytes       INTEGER NOT NULL DEFAULT 0,
    public_url       TEXT,
    status           TEXT NOT NULL DEFAULT 'pending',
    rejection_reason TEXT,
    verified_at      DATETIME,
    expires_at       DATETIME NOT NULL,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(project_id, object_key)
);
```

### webhook_deliveries

```sql
CREATE TABLE webhook_deliveries (
    id           TEXT PRIMARY KEY,
    upload_id    TEXT NOT NULL REFERENCES uploads(id),
    project_id   TEXT NOT NULL,
    url          TEXT NOT NULL,
    status_code  INTEGER,
    attempt      INTEGER NOT NULL DEFAULT 1,
    succeeded    INTEGER NOT NULL DEFAULT 0,
    error        TEXT,
    delivered_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

---

## Background Jobs

**Expired upload sweeper** — runs every 10 minutes. Finds all uploads with `status = pending` and `expires_at` in the past. For each, instructs SeaweedFS via Filer gRPC `DeleteEntry` to delete the object (ignores not found). Marks the record rejected with reason `expired`.

**Webhook retry worker** — runs every 60 seconds. Finds undelivered webhook attempts under 5 tries. Retries with backoff.

---

## Concurrency Model

Every HTTP request is handled in its own goroutine. SQLite serialises writes automatically. Reads in WAL mode are non-blocking. The SeaweedFS gRPC client uses a single persistent connection with HTTP/2 multiplexing — multiple concurrent gRPC calls share one connection without contention. The HTTP client for Filer data operations uses a pooled transport with 64 idle connections.

---

## Object Key Format

```
{media_class}/{project_id}/{year}/{month}/{uuid}.{ext}
```

The original filename never appears in the key.

---

## Environment Variables

```
OPENSTORE_PORT                       HTTP port. Default: 8080 (hardcoded)
OPENSTORE_DB_PATH                    SQLite file path. Required.
OPENSTORE_SEAWEEDFS_FILER_ADDR       SeaweedFS Filer gRPC address. Required. Format: host:port
OPENSTORE_SEAWEEDFS_FILER_HTTP_ADDR  SeaweedFS Filer HTTP address. Default: http://seaweedfs:8888
OPENSTORE_API_KEY                    Shared API key. Required.
OPENSTORE_CLAMAV_URL                 ClamAV daemon TCP address. Required.
OPENSTORE_CLAMAV_ENABLED             Enable antivirus. Default: true
OPENSTORE_PRESIGN_TTL_DEFAULT        Default upload token TTL seconds. Default: 300
OPENSTORE_PRESIGN_TTL_MAX            Maximum upload token TTL seconds. Default: 86400
OPENSTORE_READ_TTL_DEFAULT           Default read token TTL seconds. Default: 900
OPENSTORE_LOG_LEVEL                  debug/info/warn/error. Default: info
```

---

## Directory Structure

```
openstore/
  cmd/openstore/main.go
  scripts/keygen/main.go
  scripts/migrate/main.go
  internal/
    config/config.go
    db/
      db.go
      migrations/
        000001_create_projects.up.sql
        000001_create_projects.down.sql
        000002_create_buckets.up.sql
        000002_create_buckets.down.sql
        000003_create_uploads.up.sql
        000003_create_uploads.down.sql
        000004_create_webhook_deliveries.up.sql
        000004_create_webhook_deliveries.down.sql
    models/
      project.go
      bucket.go
      upload.go
    handlers/
      configure.go
      upload.go
      upload_stream.go
      helpers.go
      types.go
      webhook.go
      files.go
      health.go
    middleware/
      auth.go
      logging.go
      recovery.go
    seaweedfs/
      client.go
      write.go
      read.go
      delete.go
    security/
      magicbytes.go
      mime.go
      token.go
    webhook/
      deliver.go
    quota/
      quota.go
  docker-compose.yml
  Dockerfile
  .env.example
```
