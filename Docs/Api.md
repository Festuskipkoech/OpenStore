# OpenStore — API Reference

## Base URL

All endpoints are served from the OpenStore binary directly.

```
http://localhost:8080
```

In production this sits behind a reverse proxy (Caddy or Nginx) with TLS.

---

## Authentication

All protected routes require the API key in the Authorization header.

```
Authorization: Bearer <OPENSTORE_API_KEY>
```

The API key is generated once with `make key` and set in your `.env` file. The
`GET /health` and `GET /health/deep` endpoints are public and require no
Authorization header.

---

## Error Format

All errors return JSON with a consistent shape.

```json
{
  "error": "human readable message",
  "code": "machine_readable_code"
}
```

Common codes: `unauthorized`, `not_found`, `conflict`, `quota_exceeded`,
`mime_not_allowed`, `size_exceeded`, `verification_failed`, `invalid_request`,
`internal_error`.

---

## Endpoints

---

### POST /configure

Create a project and its buckets in a single atomic operation. If any bucket
fails validation the entire request is rolled back and nothing is persisted.

**Request**

```json
{
  "project": {
    "name": "mediavault",
    "webhook_url": "https://mediavault.example.com/webhooks/openstore",
    "webhook_secret": "a-secret-the-client-uses-to-verify-webhook-calls",
    "allowed_origins": ["https://mediavault.example.com"],
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
}
```

**Project fields**

`name` — string, required. Human label for the project.
`webhook_url` — string, required. OpenStore will POST upload results here.
`webhook_secret` — string, required. Used to sign webhook payloads.
`allowed_origins` — array of strings, required. Must not be empty.
`quota_bytes` — integer, optional. Total storage ceiling in bytes. 0 means
unlimited.

**Bucket fields**

`name` — string, required. Must be unique across all projects.
`media_class` — string, required. One of: `images`, `videos`, `audio`,
`documents`. Controls which magic byte signatures are applied during
verification. Cannot be changed after creation.
`allowed_mime` — array of strings, required. Must not be empty. Only these MIME
types may be uploaded to this bucket.
`max_bytes` — integer, required. Must be greater than zero. Per-file size ceiling
in bytes.
`presign_ttl_seconds` — integer, optional. How long a presigned upload URL
remains valid. Defaults to `OPENSTORE_PRESIGN_TTL_DEFAULT`.
`read_ttl_seconds` — integer, optional. How long a presigned read URL remains
valid for private buckets. Defaults to `OPENSTORE_READ_TTL_DEFAULT`.
`access` — string, optional. One of: `public`, `private`. Defaults to `public`.
Cannot be changed after creation.

**Response 201**

```json
{
  "project_id": "01J4KXQZ7FGHE3N2P8RV5TYW6M",
  "name": "mediavault",
  "webhook_url": "https://mediavault.example.com/webhooks/openstore",
  "allowed_origins": ["https://mediavault.example.com"],
  "quota_bytes": 10737418240,
  "used_bytes": 0,
  "buckets": [
    {
      "bucket_id": "01J4M2NQXZ...",
      "project_id": "01J4KXQZ7F...",
      "name": "mediavault-avatars",
      "media_class": "images",
      "allowed_mime": ["image/jpeg", "image/png", "image/webp"],
      "max_bytes": 5242880,
      "presign_ttl_seconds": 300,
      "read_ttl_seconds": 900,
      "access": "public",
      "created_at": "2026-07-01T12:00:00Z"
    }
  ],
  "created_at": "2026-07-01T12:00:00Z"
}
```

**Errors**

`400` — missing required fields, invalid `media_class`, invalid `access`, empty
`allowed_mime`, `max_bytes` not greater than zero, or no buckets provided.
`401` — invalid or missing API key.
`409` — a bucket name already exists.

---

### GET /configure

Retrieve project details, all buckets, and current quota usage.

**Query parameters**

`project_id` — string, required.

**Response 200**

```json
{
  "project_id": "01J4KXQZ7FGHE3N2P8RV5TYW6M",
  "name": "mediavault",
  "webhook_url": "https://mediavault.example.com/webhooks/openstore",
  "allowed_origins": ["https://mediavault.example.com"],
  "quota_bytes": 10737418240,
  "used_bytes": 524288000,
  "buckets": [...],
  "created_at": "2026-07-01T12:00:00Z"
}
```

**Errors**

`400` — `project_id` not provided.
`401` — invalid or missing API key.
`404` — project not found.

---

### PUT /configure

Reconcile project fields and buckets. Upserts the project and each bucket in the
request body. Buckets present in the database but absent from the request body
are left untouched — they are never deleted by this endpoint. To remove a bucket
use `DELETE /configure/buckets/:bucket_name`.

**Request**

```json
{
  "project_id": "01J4KXQZ7FGHE3N2P8RV5TYW6M",
  "project": {
    "name": "mediavault",
    "webhook_url": "https://mediavault.example.com/webhooks/openstore",
    "webhook_secret": "updated-secret",
    "allowed_origins": ["https://mediavault.example.com"],
    "quota_bytes": 21474836480
  },
  "buckets": [
    {
      "name": "mediavault-avatars",
      "media_class": "images",
      "allowed_mime": ["image/jpeg", "image/png", "image/webp", "image/gif"],
      "max_bytes": 5242880,
      "presign_ttl_seconds": 300,
      "access": "public"
    }
  ]
}
```

`project_id` — string, required.
All other fields follow the same rules as `POST /configure`. `media_class` and
`access` on existing buckets are ignored — they cannot be changed after creation.

**Response 200**

Same shape as `POST /configure` response, reflecting the current state after
reconciliation.

**Errors**

`400` — `project_id` not provided or invalid bucket fields.
`401` — invalid or missing API key.
`404` — project not found.
`409` — a new bucket name conflicts with an existing bucket in another project.

---

### PATCH /configure/buckets/:bucket_name

Partially update a bucket. Only `allowed_mime`, `max_bytes`,
`presign_ttl_seconds`, and `read_ttl_seconds` may be changed. Sending `access`
or `media_class` returns 400 — these are immutable after creation.

**Request**

```json
{
  "project_id": "01J4KXQZ7FGHE3N2P8RV5TYW6M",
  "allowed_mime": ["image/jpeg", "image/png", "image/webp", "image/gif"],
  "max_bytes": 10485760,
  "presign_ttl_seconds": 600
}
```

`project_id` — string, required.
All other fields are optional. Only provided fields are updated.

**Response 200**

```json
{
  "bucket_id": "01J4M2NQXZ...",
  "project_id": "01J4KXQZ7F...",
  "name": "mediavault-avatars",
  "media_class": "images",
  "allowed_mime": ["image/jpeg", "image/png", "image/webp", "image/gif"],
  "max_bytes": 10485760,
  "presign_ttl_seconds": 600,
  "read_ttl_seconds": 900,
  "access": "public",
  "created_at": "2026-07-01T12:00:00Z"
}
```

**Errors**

`400` — `project_id` not provided, or `access` or `media_class` present in body.
`401` — invalid or missing API key.
`404` — bucket not found for this project.

---

### DELETE /configure

Delete a project and all associated buckets and uploads. This is irreversible.
Requires a `confirm` field matching the project name exactly — a mismatch returns
400 and leaves the project untouched.

**Request**

```json
{
  "project_id": "01J4KXQZ7FGHE3N2P8RV5TYW6M",
  "confirm": "mediavault"
}
```

**Response 200**

```json
{
  "project_id": "01J4KXQZ7FGHE3N2P8RV5TYW6M",
  "deleted": true
}
```

**Errors**

`400` — `project_id` not provided, or `confirm` does not match project name.
`401` — invalid or missing API key.
`404` — project not found.

---

### DELETE /configure/buckets/:bucket_name

Delete a single bucket. Blocked if the bucket has verified uploads unless
`force: true` is passed.

**Request**

```json
{
  "project_id": "01J4KXQZ7FGHE3N2P8RV5TYW6M",
  "force": false
}
```

**Response 200**

```json
{
  "bucket_name": "mediavault-avatars",
  "deleted": true
}
```

**Errors**

`400` — `project_id` not provided, or bucket has verified uploads and `force` is
not `true`.
`401` — invalid or missing API key.
`404` — bucket not found for this project.

---

### POST /upload/presign

Request a presigned PUT URL for a direct upload to OpenStore.
Called by the client backend. Requires API key.

**Request**

```json
{
  "bucket_name": "mediavault-avatars",
  "filename": "profile-photo.jpg",
  "mime_type": "image/jpeg",
  "file_size": 204800
}
```

`bucket_name` — string, required.
`filename` — string, required. Used for the file extension only. The original
name never appears in the object key.
`mime_type` — string, required. Must be in the bucket's `allowed_mime` list.
`file_size` — integer, required. Must not exceed the bucket's `max_bytes` or the
project's remaining quota.

**Response 201**

```json
{
  "upload_id": "01J4M3PQXZ...",
  "upload_url": "http://seaweedfs:8333/mediavault-avatars/images/01J.../2026/07/01J....jpg?X-Amz-Algorithm=...",
  "method": "PUT",
  "object_key": "images/01J.../2026/07/01J....jpg",
  "bucket": "mediavault-avatars",
  "expires_at": "2026-07-01T12:10:00Z",
  "headers": {
    "Content-Type": "image/jpeg",
    "Content-Length": "204800"
  }
}
```

`upload_id` — pass this to `POST /upload/confirm` after the PUT succeeds.
`upload_url` — PUT the file body directly to this URL.
`headers` — must be included on the PUT request.
`expires_at` — the presigned URL is invalid after this time.

**Errors**

`400` — missing fields or invalid `file_size`.
`401` — invalid or missing API key.
`404` — bucket not found.
`422` — `mime_type` not in bucket's `allowed_mime` list.
`422` — `file_size` exceeds bucket's `max_bytes`.
`429` — `file_size` would exceed project quota.

---

### POST /upload/confirm

Notify OpenStore that the PUT to SeaweedFS has completed. OpenStore independently
verifies the upload — this call is a trigger, not trusted evidence of success.

**Request**

```json
{
  "upload_id": "01J4M3PQXZ..."
}
```

**Response 200 — verification passed**

```json
{
  "upload_id": "01J4M3PQXZ...",
  "status": "verified",
  "object_key": "images/01J.../2026/07/01J....jpg",
  "bucket": "mediavault-avatars",
  "public_url": "http://seaweedfs:8333/mediavault-avatars/images/...",
  "content_type": "image/jpeg",
  "size_bytes": 204800,
  "verified_at": "2026-07-01T12:07:32Z"
}
```

`public_url` is populated only when the bucket `access` is `public`. It is null
for private buckets.

**Response 422 — verification failed**

```json
{
  "upload_id": "01J4M3PQXZ...",
  "status": "rejected",
  "error": "magic bytes do not match declared MIME type image/jpeg",
  "code": "verification_failed"
}
```

When verification fails OpenStore deletes the object from SeaweedFS immediately
and marks the upload record rejected with the reason stored.

**Errors**

`400` — missing `upload_id`.
`401` — invalid or missing API key.
`404` — upload not found.
`409` — upload already verified or rejected.
`422` — verification failed. Object deleted from SeaweedFS.

---

### GET /uploads/:upload_id

Retrieve the current status of an upload. Useful for polling when webhook
delivery is delayed.

**Response 200**

```json
{
  "upload_id": "01J4M3PQXZ...",
  "bucket": "mediavault-avatars",
  "object_key": "images/01J.../2026/07/01J....jpg",
  "original_name": "profile-photo.jpg",
  "content_type": "image/jpeg",
  "size_bytes": 204800,
  "public_url": "http://seaweedfs:8333/...",
  "status": "verified",
  "verified_at": "2026-07-01T12:07:32Z",
  "created_at": "2026-07-01T12:07:00Z"
}
```

`public_url` is null when status is `pending`, `rejected`, or the bucket is
private.

**Errors**

`401` — invalid or missing API key.
`404` — upload not found.

---

### DELETE /files/:upload_id

Delete a file from SeaweedFS and mark the upload record deleted. Decrements the
project's `used_bytes` by the file's `size_bytes`. Only verified uploads can be
deleted.

**Response 200**

```json
{
  "upload_id": "01J4M3PQXZ...",
  "deleted": true
}
```

**Errors**

`401` — invalid or missing API key.
`404` — upload not found or not in verified state.

---

### GET /health

Shallow health check. Returns 200 if the process is alive. No authentication
required.

**Response 200**

```json
{
  "status": "ok",
  "version": "1.0.0"
}
```

---

### GET /health/deep

Deep health check. Pings SQLite and SeaweedFS independently. Returns 200 only if
both are healthy. Returns 503 if either is degraded. No authentication required.

**Response 200**

```json
{
  "status": "ok",
  "checks": {
    "sqlite": {
      "status": "ok",
      "latency_ms": 1
    },
    "seaweedfs": {
      "status": "ok",
      "latency_ms": 14
    }
  }
}
```

**Response 503**

```json
{
  "status": "degraded",
  "checks": {
    "sqlite": {
      "status": "ok",
      "latency_ms": 1
    },
    "seaweedfs": {
      "status": "error",
      "error": "dial tcp: connection refused",
      "latency_ms": 0
    }
  }
}
```

---

## Object Key Format

OpenStore generates all object keys. The client never constructs them.

```
{media_class}/{project_id}/{year}/{month}/{ulid}.{ext}
```

Example:

```
images/01J4KXQZ7F.../2026/07/01J4M3PQXZ....jpg
```

The original filename never appears in the key. This prevents path traversal,
enumeration, and naming collisions.

---

## Bucket Access Policy

Public buckets — OpenStore returns a permanent `public_url` after verification.

Private buckets — no permanent URL is generated. `public_url` is always null.
The client backend requests presigned read URLs through OpenStore as needed.
SeaweedFS is never directly reachable from the frontend.

---

## Rate Limiting

OpenStore does not implement rate limiting internally. Deploy behind a reverse
proxy (Caddy, Nginx, Traefik) and apply rate limiting there per client IP or per
Authorization header.
