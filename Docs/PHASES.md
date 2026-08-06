# OpenStore — Execution Phases

## Overview

Eight phases. Each phase has a clear deliverable, a set of acceptance criteria, and leaves the codebase in a working and testable state. No phase produces code that cannot be run or tested by the end of it.

The phases are ordered so that each one builds on a stable foundation. Skipping a phase or starting a later phase before the prior one passes its acceptance criteria will create instability that compounds forward.

---

## Phase 1 — Foundation

**Goal:** The binary starts, serves requests, enforces authentication, and passes health checks. No business logic yet.

**What gets built:**

- Module initialisation — `go.mod`, dependency graph, directory structure matching the layout in ARCHITECTURE.md
- Environment variable parsing in `internal/config/config.go` — all variables from the environment table, with validation on startup for required values
- SQLite connection in `internal/db/db.go` — WAL mode enabled, golang-migrate wired with embedded migration files, `m.Up()` called on open
- First migration pair — `000001_create_projects.up.sql` and down — even if the table is empty, the migration infrastructure must prove it works
- Chi router skeleton in `cmd/openstore/main.go` — routes mounted but handlers not yet implemented beyond health
- Middleware stack — authentication (`internal/middleware/auth.go`) using constant-time API key comparison, structured request logging, panic recovery
- Health handlers — `GET /health` shallow, `GET /health/deep` pinging SQLite and SeaweedFS with latency measurement
- SeaweedFS client in `internal/seaweedfs/client.go` — establishes the Filer gRPC connection on port 18888 and holds an internal HTTP client pointed at the Filer HTTP API on port 8888. `Ping()` uses gRPC and is called by the deep health check
- Key generation script — `scripts/keygen/main.go` outputting a `ops_live_` prefixed cryptographically secure random string
- Docker Compose with SeaweedFS and OpenStore — SeaweedFS ports 8888 and 18888 both use `expose`
- `.env.example` with all variables including `OPENSTORE_SEAWEEDFS_FILER_HTTP_ADDR`

**Acceptance criteria:**

- `go build ./...` produces no errors
- `go test ./...` passes with no test files yet (or skeleton tests)
- `curl http://localhost:8080/health` returns `{"status":"ok","version":"1.0.0"}`
- `curl http://localhost:8080/health/deep` returns 200 with both checks ok when SeaweedFS is running, 503 when SeaweedFS is stopped
- Any request without a valid API key returns 401
- `GET /health` and `GET /health/deep` return 200 without any Authorization header
- Binary exits on startup if `OPENSTORE_SEAWEEDFS_FILER_ADDR`, `OPENSTORE_API_KEY`, `OPENSTORE_DB_PATH`, or `OPENSTORE_CLAMAV_URL` are missing

---

## Phase 2 — Configuration

**Goal:** The system can be fully configured via the API. Project and bucket records are created, read, updated, and deleted. The configuration lifecycle works end to end.

**What gets built:**

- Remaining migrations — `000002_create_buckets.up/down.sql`, bucket schema with all columns including `access`, `presign_ttl_seconds`, `read_ttl_seconds`
- Project model in `internal/models/project.go` — struct, `Create`, `Get`, `Update`, `Delete` DB methods
- Bucket model in `internal/models/bucket.go` — struct, `Create`, `GetByName`, `GetAllForProject`, `Update`, `Delete` DB methods
- Configure handlers in `internal/handlers/configure.go`:
  - `POST /configure` — atomic project + buckets creation in a single transaction, validation of all fields, media class allowlist check, access value check
  - `GET /configure` — project with all buckets and current `used_bytes`
  - `PUT /configure` — reconciliation logic, upserts project fields, upserts buckets, does not delete absent buckets
  - `PATCH /configure/buckets/:bucket_name` — partial bucket update, blocks changes to `access` and `media_class`
  - `DELETE /configure` — requires confirm field matching project name
  - `DELETE /configure/buckets/:bucket_name` — blocked if verified uploads exist unless `force: true`
- Full integration test suite for all configure endpoints as specified in TESTING.md

**Acceptance criteria:**

- All configure integration tests pass
- `POST /configure` with one invalid bucket rolls back — project is not in the database
- `PUT /configure` does not delete a bucket that is in the database but absent from the request body
- `PATCH /configure/buckets/:name` returns 400 if `access` or `media_class` are present in the request body
- `DELETE /configure` with wrong confirm value returns 400 and project is untouched
- `DELETE /configure/buckets/:name` with verified uploads and no `force` returns 400
- `go test ./internal/handlers/... -run Configure` all pass

---

## Phase 3 — Upload Presign

**Goal:** The client backend can request signed upload tokens. OpenStore returns a URL pointing to its own upload endpoint. The full TTL resolution chain works. Quota headroom is checked before a token is issued.

**What gets built:**

- Upload migration — `000003_create_uploads.up/down.sql`, full schema with indexes
- Upload model in `internal/models/upload.go` — struct, `Create`, `GetByID`, `UpdateStatus` DB methods
- Upload token signing in `internal/security/token.go` — `Sign(claims) string` and `Verify(token, claims) error` using HMAC-SHA256 keyed with the API key. The token payload contains `upload_id`, `bucket_name`, `mime_type`, `file_size`, and `expires_at` as a Unix timestamp
- TTL resolution logic in `internal/config/ttl.go` — three-layer resolution: per-request override → per-bucket default → global default, with hard `OPENSTORE_PRESIGN_TTL_MAX` ceiling applied regardless
- Quota check in `internal/quota/quota.go` — `CheckHeadroom(projectID, fileSize)` reading current `used_bytes` and `quota_bytes`
- MIME allowlist check in `internal/security/mime.go` — `IsAllowed(mimeType, allowedList)` using exact string match
- Upload presign handler — `POST /upload/presign` in `internal/handlers/upload.go`. Validates bucket, MIME, size, and quota headroom, then creates a pending upload record and returns a signed URL of the form `PUT /upload/{upload_id}?token=<hmac_signed_token>` pointing to OpenStore itself
- Unit tests for TTL resolution, quota calculation, MIME allowlist, and token sign/verify
- Integration tests for presign endpoint as specified in TESTING.md

**Acceptance criteria:**

- Unit tests for TTL resolution covering all five cases in TESTING.md pass
- Unit tests for quota calculation covering all five cases pass
- Unit tests for MIME allowlist covering all four cases pass
- Unit tests for token sign and verify — valid token passes, tampered payload fails, expired token fails
- Integration tests for `POST /upload/presign` all pass
- The upload URL returned points to `PUT /upload/{upload_id}` on OpenStore — not to SeaweedFS
- Upload record is created in the database with `status: pending` and correct `expires_at`
- Presign is rejected before a token is issued when quota would be exceeded
- `go test ./internal/security/... ./internal/handlers/... -run Presign` all pass

---

## Phase 4 — Upload Streaming and Verification

**Goal:** The browser can PUT a file to OpenStore. OpenStore streams the bytes to SeaweedFS via the Filer HTTP API, runs the full in-stream and post-stream security verification chain, deducts quota atomically, and marks the upload verified or rejected. Both public and private bucket outcomes are handled correctly.

**What gets built:**

- SeaweedFS write in `internal/seaweedfs/write.go` — `WriteObject(ctx, objectKey, contentType string, r io.Reader) error` streaming bytes to SeaweedFS via an HTTP PUT to the Filer on port 8888. The Filer handles volume assignment, chunking, replication, and metadata commit internally. No intermediate buffering — the request body is piped directly.
- SeaweedFS delete in `internal/seaweedfs/read.go` — `DeleteObject(ctx, objectKey string) error` via Filer gRPC `DeleteEntry` on port 18888, treating a not-found response as success
- SeaweedFS read in `internal/seaweedfs/read.go` — `ReadObject(ctx, objectKey string, w io.Writer) error` fetching the object via Filer HTTP GET on port 8888 and streaming bytes to the provided writer
- Magic byte signatures in `internal/security/magicbytes.go` — `VerifyMagicBytes(mimeType string, header []byte) error` covering all MIME types from ARCHITECTURE.md. The function receives the first 12 bytes read from the stream. MP4 is checked at offset 4. WebP is checked at offset 0 for RIFF and offset 8 for WEBP
- Quota deduction in `internal/quota/quota.go` — `Deduct(tx, projectID, bytes)` inside a SQLite transaction
- Upload stream handler — `PUT /upload/{upload_id}` in `internal/handlers/upload_stream.go` running the full ten-step verification chain from ARCHITECTURE.md:
  - Token signature and claims validated before any bytes are accepted
  - Ownership check — upload record must belong to the authenticated project
  - Status gate — upload must be in `pending` state
  - Content-Type and Content-Length headers validated against token claims
  - Bytes streamed to SeaweedFS via Filer HTTP while counting bytes received; stream aborted and object deleted via gRPC if size limit exceeded
  - First 12 bytes teed from the stream for magic byte verification
  - After stream completes — magic bytes checked, ClamAV scan triggered, deep content inspection run
  - On any failure — `DeleteObject` called via Filer gRPC, upload marked rejected with reason, webhook fired
  - On success — quota deducted in SQLite transaction, upload marked verified, public URL stored for public buckets, object key stored for private buckets, webhook fired in goroutine
- Handler types extracted to `internal/handlers/types.go`, IO and string helpers to `internal/handlers/helpers.go`
- This route lives outside the API key auth middleware group — the browser authenticates via the HMAC token in the query string, not the API key
- Unit tests for magic bytes covering all MIME types including MP4 offset and WebP dual-check cases
- Integration tests for the upload stream handler using a mock SeaweedFS client

**Acceptance criteria:**

- Magic byte unit tests pass for all MIME types — both valid and tampered inputs
- MP4 offset-4 test passes specifically — bytes at position 4 must match, bytes at position 0 must not
- WebP test passes — RIFF at offset 0 alone is not sufficient, WEBP at offset 8 must also match
- Integration tests for `PUT /upload/{upload_id}` all pass
- On magic byte mismatch, `DeleteObject` is called via gRPC exactly once
- On success with a public bucket, `public_url` is populated on the upload record
- On success with a private bucket, `public_url` is null on the upload record
- `used_bytes` on the project is incremented by exactly `size_bytes` after a successful upload
- Submitting the same `upload_id` a second time returns 409
- On size limit exceeded mid-stream, `DeleteObject` is called and the upload is rejected with a 413
- `go test ./internal/security/... ./internal/handlers/... -run Upload` all pass

---

## Phase 5 — Webhook

**Goal:** The client backend receives reliable webhook delivery after every upload outcome. Delivery is async and does not block the upload response. Retries follow the backoff schedule. All attempts are recorded.

**What gets built:**

- Webhook deliveries migration — `000004_create_webhook_deliveries.up/down.sql`
- Webhook delivery model in `internal/models/webhook_delivery.go` — struct, `Create`, `UpdateAttempt` DB methods
- Webhook deliver in `internal/webhook/deliver.go`:
  - `Deliver(upload, project)` — fires in a goroutine, POSTs JSON payload to `webhook_url`, includes `X-OpenStore-Signature` header with HMAC-SHA256 over the raw request body signed with `webhook_secret`, records delivery attempt in DB
  - Payload shape for verified and rejected events matching the webhook payload spec in ARCHITECTURE.md
- Webhook retry worker in `internal/webhook/deliver.go` — background goroutine started from `main.go`, runs every 60 seconds, finds undelivered deliveries under 5 attempts, applies backoff check, retries
- HMAC signing utility — `Sign(secret, body)` and `Verify(secret, body, signature)`
- Integration tests for delivery and retry as specified in TESTING.md using a mock webhook receiver started with `httptest.NewServer`
- Unit tests for HMAC signing and verification

**Acceptance criteria:**

- `PUT /upload/{upload_id}` returns its response before webhook delivery completes — verified by timing in integration tests
- Webhook POST is received by mock receiver within 100ms of upload response in integration tests
- Mock receiver returning 500 triggers a retry after the configured backoff
- After 5 failed attempts, `succeeded` remains 0 and no further retries are scheduled
- Successful retry sets `succeeded = 1` in `webhook_deliveries`
- HMAC unit tests for valid signature, wrong secret, and tampered body all pass
- `go test ./internal/webhook/...` all pass

---

## Phase 6 — File Reads and Deletion

**Goal:** The frontend can read both public and private files through OpenStore. OpenStore proxies the bytes from SeaweedFS via the Filer HTTP API and streams them to the caller. Files can be deleted with correct quota decrement.

**What gets built:**

- Files handler in `internal/handlers/files.go`:
  - `GET /files/{upload_id}` — validates the upload exists and is in verified state, fetches the object from SeaweedFS via `ReadObject` (Filer HTTP GET on port 8888), streams bytes back to the caller with the correct `Content-Type` header. For private buckets, validates the request is authorised before streaming
  - `DELETE /files/{upload_id}` — validates upload is verified, calls `DeleteObject` on SeaweedFS via Filer gRPC `DeleteEntry`, decrements `used_bytes` in a SQLite transaction, marks record deleted
- Upload status endpoint — `GET /uploads/{upload_id}` returning current upload state
- `PublicURL` on the SeaweedFS client wired to `OPENSTORE_PUBLIC_BASE_URL` for permanent public file URLs
- Integration tests for all three endpoints as specified in TESTING.md

**Acceptance criteria:**

- `GET /files/{upload_id}` on a public bucket streams the file bytes with the correct `Content-Type`
- `GET /files/{upload_id}` on a private bucket without authorisation returns 403
- `GET /files/{upload_id}` on a pending or rejected upload returns 404
- The bytes streamed from a real SeaweedFS instance in E2E context match the original upload exactly
- `DELETE /files/{upload_id}` decrements `used_bytes` by exactly the file's `size_bytes`
- `DELETE /files/{upload_id}` on an already-deleted upload returns 404
- `go test ./internal/handlers/... -run Files` all pass

---

## Phase 7 — Background Jobs

**Goal:** Expired pending uploads are cleaned up automatically. Orphaned SeaweedFS objects are deleted. Webhook retries fire on schedule.

**What gets built:**

- Expired upload sweeper goroutine started from `main.go`:
  - Runs every 10 minutes
  - Finds all uploads with `status = pending` and `expires_at < now()`
  - For each: calls `DeleteObject` via Filer gRPC `DeleteEntry` (treats not-found as success), sets `status = rejected`, sets `rejection_reason = expired`
  - Logs each cleaned record at info level
- Sweeper and retry worker integration tests as specified in TESTING.md
- Manual migration control script — `scripts/migrate/main.go` accepting `up`, `down N`, and `version` subcommands for operational use

**Acceptance criteria:**

- Sweeper integration test inserts an expired pending upload, triggers the sweeper, verifies status is rejected and mock gRPC received the delete call
- Sweeper does not touch verified or already-rejected records
- Sweeper handles a missing SeaweedFS object (gRPC not-found) without error or panic
- Retry worker integration test verifies backoff timing is respected — an attempt=2 record with a delivery less than 10 seconds ago is not retried
- `go test ./internal/handlers/... -run Jobs` all pass
- `go run scripts/migrate/main.go version` prints the current applied migration version

---

## Phase 8 — End-to-End, Hardening, and Release

**Goal:** The full system is validated against real infrastructure, error surfaces are consistent, the binary is production-ready, and documentation matches the implementation.

**What gets built:**

- Test fixtures in `tests/fixtures/` — valid and tampered files for every supported MIME type as specified in TESTING.md
- Full E2E test suite in `tests/e2e/` — upload flow, private read flow, deletion flow, configure lifecycle, concurrent upload stress test
- Error response audit — every handler reviewed to ensure error codes match the documented list in API.md, no handler returns a bare string error
- Concurrent stress test verifying `used_bytes` accuracy under 50 simultaneous uploads from the same project
- Dockerfile — single-stage Go build, non-root user, no shell
- Production deployment notes verified — Caddy reverse proxy config, SeaweedFS internal network isolation with both ports 8888 and 18888 using `expose`
- Final pass comparing all documentation files against actual implemented behaviour, correcting any drift

**Acceptance criteria:**

- `go test ./tests/e2e/... -tags e2e -v` all pass against a running Docker Compose stack
- Concurrent stress test: 50 uploads complete, `used_bytes` equals exact sum of all file sizes
- `go test ./... -coverprofile=coverage.out` meets coverage targets from TESTING.md
- `docker build .` produces a working image
- `docker compose up -d && curl http://localhost:8080/health/deep` returns 200 within 10 seconds of startup
- All error responses across all endpoints match the codes documented in API.md
- No handler returns a 500 for a client error — all client errors are 4xx with a machine-readable code
- README quick start instructions work exactly as written on a clean clone with no prior setup
