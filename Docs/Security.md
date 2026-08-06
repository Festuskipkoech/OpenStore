# OpenStore — Security

## Overview

OpenStore enforces security at multiple independent layers. No single layer is trusted alone. A file must pass every layer before it is marked verified and the client backend is notified. Failure at any layer results in immediate deletion from SeaweedFS and a rejected status on the upload record.

The security model assumes the following threat surface:

- A malicious user attempting to upload prohibited file types
- A malicious user attempting to upload valid file types containing embedded malicious payloads
- A malicious user attempting to forge or replay an upload token
- A malicious actor attempting to spoof webhook delivery to the client backend
- A client backend that has been compromised attempting to exceed its quota or access another project's files
- Anyone attempting to reach SeaweedFS directly — this is prevented by network isolation

OpenStore does not trust the browser, does not trust the client backend's assertions about a file, and does not trust the upload token as evidence of a clean file. Every claim is independently verified server-side during the upload stream.

---

## Layer 1 — Network Isolation

SeaweedFS is the most important thing to protect. It is never reachable from the internet, from the client backend, or from the frontend. It lives exclusively on the internal Docker network and is reachable only by OpenStore.

OpenStore communicates with SeaweedFS over two internal ports, both using `expose` in docker-compose:

- **Port 8888 (Filer HTTP)** — data operations: writing file bytes via `PUT /{objectKey}` and reading them via `GET /{objectKey}`. The Filer HTTP API is SeaweedFS's first-class interface for data transfer — it handles volume assignment, chunking, replication, and metadata commit internally in a single call.
- **Port 18888 (Filer gRPC)** — metadata operations: `DeleteEntry`, `LookupDirectoryEntry`, and the liveness ping used by the deep health check.

The SeaweedFS S3 API is not used.

In `docker-compose.yml`, all SeaweedFS ports use `expose` not `ports`. `expose` makes ports available only to other containers on the same Docker network. `ports` would map them to the host machine and potentially the internet.

OpenStore is the only public surface. Port 8080 is the single entry point for all external traffic. In production this sits behind a TLS-terminating reverse proxy.

```
Internet
    |
  443/TLS  (reverse proxy — Caddy or Nginx)
    |
  8080  (OpenStore — only container with a public port)
    |
  8888  (SeaweedFS Filer HTTP — internal Docker network only, never public)
 18888  (SeaweedFS Filer gRPC — internal Docker network only, never public)
```

---

## Layer 2 — API Key Authentication

Every request from the client backend to OpenStore carries the shared API key:

```
Authorization: Bearer <OPENSTORE_API_KEY>
```

OpenStore compares the incoming key using `subtle.ConstantTimeCompare` — constant-time equality that prevents timing attacks where an attacker measures response latency to determine how many leading characters of a key are correct.

The API key never appears in logs. The logging middleware does not record the `Authorization` header.

The frontend never holds the API key. The key lives exclusively in server-side environments.

**Key generation** uses `crypto/rand` to generate 32 bytes of cryptographically secure random data, encoded as hex with a recognisable `ops_live_` prefix. The prefix makes it easy to detect accidental exposure in logs, error reports, or version control, and to write secret scanning rules in CI.

---

## Layer 3 — Upload Token Authentication

When the client backend calls `POST /upload/presign`, OpenStore generates a signed upload token and returns a URL pointing to its own upload endpoint:

```
PUT /upload/{upload_id}?token=<hmac_signed_token>
```

The token is an HMAC-SHA256 signature computed over a payload containing:

- `upload_id`
- `bucket_name`
- `mime_type`
- `file_size`
- `expires_at` (Unix timestamp)

Signed with the API key. OpenStore generates and validates all tokens itself using `internal/security/token.go`. There is no dependency on SeaweedFS for token generation — SeaweedFS has no public address and is never contacted during presigning.

When the frontend PUTs to the upload endpoint, OpenStore validates:

- The HMAC signature is correct
- The token has not expired
- The `upload_id` in the URL matches the one embedded in the token
- The `Content-Type` and `Content-Length` headers match the values embedded in the token

Any mismatch on any of these returns 401 before a single byte is accepted. The token is single-use — once an upload moves out of `pending` state, the token is invalid even if it has not expired.

---

## Layer 4 — CORS

The project's `allowed_origins` list controls which browser origins can call OpenStore's upload endpoint. OpenStore includes `Access-Control-Allow-Origin` headers on presign responses that restrict cross-origin PUT requests to listed origins only.

A browser on an unlisted origin cannot complete an upload because the CORS preflight will fail before any bytes are sent.

---

## Layer 5 — Presign-Time Validation

Before issuing an upload token, OpenStore validates:

- The declared `mime_type` is in the bucket's `allowed_mime` list
- The declared `file_size` does not exceed the bucket's `max_bytes`
- The declared `file_size` does not exceed the project's remaining quota
- The bucket belongs to the authenticated project

These checks happen before a token is ever issued. They are a first gate, not the security boundary. The verification chain re-checks everything independently during the actual upload stream.

---

## Layer 6 — Upload Stream Verification Chain

This is the primary security boundary. It runs entirely inside OpenStore as the file streams in and immediately after the stream completes. The browser PUTs directly to OpenStore. OpenStore pipes the bytes to SeaweedFS via a streaming HTTP PUT to the Filer on port 8888 with no intermediate buffering — constant memory regardless of file size. The chain does not trust anything asserted during the presign phase.

### Step 1 — Token Validation

Signature, expiry, and all embedded claims are checked against the upload record and the incoming request headers before any bytes are accepted. Upload aborted immediately on any mismatch.

### Step 2 — Ownership

The upload record must belong to the project identified by the API key. Returns 404 rather than 403 to avoid leaking the existence of other projects' upload IDs.

### Step 3 — Status Gate

The upload must be in `pending` state. Prevents replay attacks where a confirmed upload token is resubmitted.

### Step 4 — Size Enforcement During Stream

OpenStore counts bytes as they arrive while piping them to SeaweedFS via Filer HTTP. If bytes received exceed the bucket's `max_bytes` or the project's remaining quota, the stream is aborted mid-transfer. OpenStore immediately calls `DeleteEntry` via Filer gRPC to remove the partial object from SeaweedFS. The client receives a 413 response.

### Step 5 — MIME Re-Check

The `Content-Type` header on the PUT request must match the declared type from the token and be in the bucket's `allowed_mime` list. Checked before accepting any bytes.

### Step 6 — Magic Byte Verification

The first 12 bytes of the incoming stream are teed into a buffer for inspection while the rest of the bytes continue piping to SeaweedFS. These bytes are compared against the known file signatures for the declared MIME type using `internal/security/magicbytes.go`.

Magic bytes are the file format's actual identity. A file can have any extension and any declared Content-Type — the bytes at the start of the file reveal what it actually is.

**Supported signatures:**

```
image/jpeg       FF D8 FF                              (offset 0)
image/png        89 50 4E 47 0D 0A 1A 0A               (offset 0)
image/webp       52 49 46 46 at offset 0 (RIFF)
                 57 45 42 50 at offset 8 (WEBP)
image/gif        47 49 46 38                            (offset 0)
video/mp4        66 74 79 70                            (offset 4 — ftyp box)
video/webm       1A 45 DF A3                            (offset 0)
audio/mpeg       49 44 33 / FF FB / FF F3 / FF F2       (offset 0)
audio/wav        52 49 46 46                            (offset 0)
audio/ogg        4F 67 67 53                            (offset 0)
audio/flac       66 4C 61 43                            (offset 0)
application/pdf  25 50 44 46                            (offset 0)
```

**MP4 special case:** The ftyp box starts at byte offset 4. OpenStore checks bytes 4-7. A file with `66 74 79 70` at offset 0 does not match.

**WebP special case:** WebP begins with RIFF at offset 0, the same signature as WAV. OpenStore checks both offset 0 for RIFF and offset 8 for the WEBP identifier. A WAV file declared as WebP passes offset 0 but fails offset 8.

If magic bytes do not match, `DeleteEntry` is called via Filer gRPC immediately and the upload is rejected with the mismatch recorded as the rejection reason.

### Step 7 — Antivirus Scan

OpenStore streams the full file to a ClamAV daemon over a TCP socket. ClamAV scans against its virus definition database and returns either `OK` or a threat name.

If ClamAV returns a threat, `DeleteEntry` is called via Filer gRPC immediately and the upload is rejected with the threat name as the rejection reason.

**Fail-closed behaviour:** if the ClamAV daemon is unreachable, the upload is rejected with reason `antivirus_unavailable` rather than proceeding without a scan. Failing open is not acceptable. `OPENSTORE_CLAMAV_ENABLED=false` disables this step for local development only and must never be set in production.

**Definition freshness:** ClamAV's `freshclam` daemon updates signatures automatically. In production, verify definitions are no more than 24 hours old.

### Step 8 — Deep Content Inspection

**Images** — decoded and re-encoded using `libvips` via the `govips` Go binding. Re-encoding strips all EXIF data, ICC profiles, XMP tags, and comments. A re-encoded image cannot carry metadata-embedded payloads. The sanitised file replaces the original in SeaweedFS via a Filer HTTP PUT. A file that cannot be decoded as its declared type fails this step even if it passed magic bytes.

**PDFs** — parsed using `pdfcpu` (pure Go, no native dependencies) to detect:
- Embedded JavaScript (`/JS`, `/JavaScript`)
- Embedded executables (`/EmbeddedFile` with executable MIME types)
- External URI actions on open (`/OpenAction` with `/URI`)
- Launch actions (`/Launch`)

If any are found, the upload is rejected with the specific element identified. OpenStore does not sanitise PDFs — rejection is correct because the intent of the upload is suspect.

**Videos and audio** — ClamAV is the primary defence. Full transcoding is out of scope for OpenStore. If the client application requires sanitised video output, trigger transcoding from the `upload.verified` webhook using a separate media processing service.

### Step 9 — Quota Deduction

`used_bytes` incremented inside a SQLite transaction. Atomic — concurrent uploads from the same project cannot both succeed if doing so would exceed the quota.

### Step 10 — Record Update and Webhook

Upload marked `verified`. For public buckets, the permanent URL is stored on the upload record. For private buckets, only the object key is stored and `public_url` remains null. Webhook fired in a goroutine — does not block the upload response.

---

## Layer 7 — Webhook Authenticity

OpenStore includes `X-OpenStore-Signature: hmac-sha256=<hex>` on every webhook delivery. The HMAC is computed over the raw request body using the project's `webhook_secret`.

The client backend must verify this signature before processing any webhook payload. Constant-time comparison required. A webhook that fails signature verification must be rejected.

The `webhook_secret` is set during configuration and never returned in any `GET /configure` response.

---

## Layer 8 — Quota Isolation

Each project has its own `quota_bytes` ceiling and `used_bytes` counter. Projects are completely isolated. One project cannot consume another's quota or access another's files.

---

## Rejection Behaviour

When any layer fails after bytes have reached SeaweedFS:

1. `DeleteEntry` called via Filer gRPC immediately
2. Upload record status set to `rejected`
3. Rejection reason stored
4. Webhook fired with `event: upload.rejected` and reason included
5. Quota not deducted

OpenStore does not log file contents. It logs upload ID, bucket, file size, and rejection reason only.

---

## What OpenStore Does Not Do

**User identity and authorisation** — OpenStore does not know who the end user is. Per-user access control is the client backend's responsibility before calling OpenStore file endpoints.

**Content moderation** — CSAM detection, hate speech, copyright detection. These require specialised services and are out of scope.

**Video sanitisation** — full transcoding belongs in the client application's media pipeline triggered by the webhook.

**DDoS protection** — deploy behind a reverse proxy with rate limiting per IP and per API key.

**Encrypted storage at rest** — configure SeaweedFS volume encryption at the infrastructure layer if required.

---

## Environment Variables

```
OPENSTORE_CLAMAV_URL                 ClamAV daemon TCP address. Required.
OPENSTORE_CLAMAV_ENABLED             Enable antivirus. Default: true. Never false in production.
OPENSTORE_SEAWEEDFS_FILER_HTTP_ADDR  Filer HTTP address for data ops. Default: http://seaweedfs:8888
```

---

## Production Security Checklist

- TLS enabled on the reverse proxy for the OpenStore endpoint
- All SeaweedFS ports use `expose` not `ports` in docker-compose — never reachable from outside Docker
- SeaweedFS Filer HTTP port 8888 and gRPC port 18888 are both internal only — neither is mapped to the host
- SeaweedFS S3 API port is not exposed and not used
- ClamAV running with `freshclam` updating definitions (max 24 hours old)
- `OPENSTORE_CLAMAV_ENABLED=true`
- API key generated with `make key`, not committed to version control
- `webhook_secret` is a strong random value not shared with any other service
- `allowed_origins` contains only exact frontend origins — no wildcards
- All projects have an explicit `quota_bytes` — no externally-facing project uses 0
- Rate limiting configured at the reverse proxy layer
- OpenStore logs shipped to centralised store — rejection events monitored and alerted on
- `OPENSTORE_LOG_LEVEL=info` in production — debug level may expose request details
