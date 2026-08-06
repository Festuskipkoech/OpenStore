# OpenStore — Testing

## Overview

OpenStore has three categories of tests. Unit tests cover pure logic with no external dependencies. Integration tests wire components together against an in-memory SQLite database and a mock SeaweedFS server running inside the test process. End-to-end tests run the full binary against a real SeaweedFS instance in Docker and are reserved for CI.

Run all tests except E2E:

```bash
go test ./...
```

Run only unit tests:

```bash
go test ./internal/security/... ./internal/quota/... ./internal/webhook/... ./internal/seaweedfs/...
```

Run integration tests:

```bash
go test ./internal/handlers/...
```

Run E2E tests (requires Docker):

```bash
go test ./tests/e2e/... -tags e2e
```

---

## Unit Tests

Unit tests live alongside the package they test. No database, no HTTP server, no SeaweedFS. Pure function input-output.

### Magic Byte Verification — `internal/security/magicbytes_test.go`

Each supported MIME type gets two test cases — one with a valid file header, one with a tampered header — plus a case where the declared MIME type and the actual bytes disagree.

| Test | Input | Expected |
|---|---|---|
| JPEG valid | `ffd8ff` prefix | pass |
| JPEG tampered | PNG bytes declared as JPEG | fail, reason stored |
| PNG valid | `89504e47` prefix | pass |
| PNG tampered | random bytes | fail |
| WebP valid | `52494646` prefix | pass |
| GIF valid | `47494638` prefix | pass |
| MP4 valid | `66747970` at offset 4 | pass |
| MP4 offset zero | `66747970` at offset 0 | fail — offset check must be enforced |
| WebM valid | `1a45dfa3` prefix | pass |
| MP3 valid | `494433` prefix | pass |
| MP3 sync word | `fffb` prefix | pass |
| WAV valid | `52494646` prefix | pass |
| OGG valid | `4f676753` prefix | pass |
| FLAC valid | `664c6143` prefix | pass |
| PDF valid | `25504446` prefix | pass |
| PDF tampered | JPEG bytes declared as PDF | fail |
| Empty bytes | zero length input | fail |
| Insufficient bytes | 3 bytes for a type needing 8 | fail |

### MIME Allowlist — `internal/security/mime_test.go`

| Test | Input | Expected |
|---|---|---|
| Allowed type | `image/jpeg` in `["image/jpeg","image/png"]` | pass |
| Disallowed type | `image/gif` not in list | fail |
| Empty allowlist | any type against `[]` | fail |
| Case sensitivity | `Image/JPEG` against lowercase list | fail — MIME types are case-sensitive |

### TTL Resolution — `internal/config/ttl_test.go`

| Test | Per-request | Bucket | Global default | Max | Expected TTL |
|---|---|---|---|---|---|
| Per-request wins | 600 | 300 | 300 | 86400 | 600 |
| Bucket wins when no per-request | 0 | 1800 | 300 | 86400 | 1800 |
| Global default fallback | 0 | 0 | 300 | 86400 | 300 |
| Per-request capped at max | 90000 | 300 | 300 | 86400 | 86400 |
| Bucket TTL capped at max | 0 | 90000 | 300 | 86400 | 86400 |

### Quota Calculation — `internal/quota/quota_test.go`

| Test | quota_bytes | used_bytes | file_size | Expected |
|---|---|---|---|---|
| Fits within quota | 10 GB | 1 GB | 500 MB | pass |
| Exactly at limit | 10 GB | 9.5 GB | 512 MB | fail |
| Exceeds by one byte | 10 GB | 10 GB - 1 | 2 | fail |
| Unlimited quota | 0 | any | any | pass |
| Zero file size | 10 GB | 5 GB | 0 | pass |

### Webhook HMAC — `internal/webhook/deliver_test.go`

| Test | Input | Expected |
|---|---|---|
| Valid signature | correct secret, correct body | verification passes |
| Wrong secret | different key | verification fails |
| Tampered body | correct secret, modified body | verification fails |
| Empty body | correct secret, empty payload | verification passes — HMAC of empty string is valid |
| Signature format | any valid payload | header is `hmac-sha256=<64 hex chars>` |

### Object Key Format — `internal/seaweedfs/presign_test.go`

| Test | Input | Expected |
|---|---|---|
| Key structure | media_class, project_id, date, uuid, filename | `{class}/{project_id}/2026/07/{uuid}.{ext}` |
| Extension extraction | `profile-photo.jpg` | `.jpg` |
| Original name absent | any filename | key does not contain original name |
| Path traversal attempt | `../../etc/passwd` | extension only, no path components in key |
| No extension | `filewithoutext` | key uses no extension suffix |

### API Key Comparison — `internal/middleware/auth_test.go`

| Test | Expected |
|---|---|
| Matching keys | pass |
| Different keys of same length | fail — constant-time comparison |
| Different keys of different length | fail |
| Empty incoming key | fail |
| Empty stored key | fail |

---

## Integration Tests

Integration tests use an in-memory SQLite database initialised with the full migration set and a mock SeaweedFS HTTP server started with `httptest.NewServer`. They test full HTTP handler behaviour including middleware, database writes, and SeaweedFS interactions.

Helper setup in `internal/handlers/testhelper_test.go`:

```go
func newTestEnv(t *testing.T) *testEnv {
    db := openInMemoryDB(t)         // runs all migrations
    seaweed := mockSeaweedFS(t)     // httptest.Server responding to S3 API calls
    router := buildRouter(db, seaweed.URL, testAPIKey)
    return &testEnv{db: db, seaweed: seaweed, router: router}
}
```

### Configure Endpoints — `internal/handlers/configure_test.go`

**POST /configure**

| Test | Setup | Expected |
|---|---|---|
| Valid full configuration | project + 3 buckets | 201, all buckets created, returned in response |
| Missing project name | no name field | 400 |
| Invalid media class | `media_class: "gifs"` | 400 |
| Invalid access value | `access: "restricted"` | 400 |
| Empty allowed_mime | `allowed_mime: []` | 400 |
| Duplicate bucket name | two buckets with same name | 400, nothing created |
| No buckets array | project only | 400 |
| Invalid API key | wrong bearer token | 401 |
| Duplicate project | call POST /configure twice | 409 |
| Atomic rollback | valid project, one invalid bucket | 409 or 400, project not in DB |
| TTL omitted | no presign_ttl_seconds | 201, bucket gets global default |
| Zero quota_bytes | unlimited | 201, quota_bytes stored as 0 |

**GET /configure**

| Test | Setup | Expected |
|---|---|---|
| Returns full config | configured project with 2 buckets | 200, project and both buckets returned |
| Includes used_bytes | verified upload exists | 200, used_bytes reflects upload size |
| Not configured | empty DB | 404 |
| Invalid API key | wrong token | 401 |

**PUT /configure**

| Test | Setup | Expected |
|---|---|---|
| Updates project fields | change webhook_url | 200, new url in DB |
| Adds new bucket | existing config + new bucket in body | 200, new bucket created |
| Updates existing bucket TTL | existing bucket with new presign_ttl_seconds | 200, TTL updated |
| Does not delete absent bucket | bucket in DB not in request body | 200, bucket still in DB |
| Invalid API key | wrong token | 401 |
| Project not found | empty DB | 404 |

**PATCH /configure/buckets/:bucket_name**

| Test | Setup | Expected |
|---|---|---|
| Updates allowed_mime | adds gif to image bucket | 200, new mime in DB |
| Updates max_bytes | raises ceiling | 200 |
| Updates presign_ttl_seconds | changes TTL | 200 |
| Attempt to change access | `access: "private"` on public bucket | 400 |
| Attempt to change media_class | `media_class: "videos"` | 400 |
| Bucket not found | wrong name | 404 |
| Invalid API key | wrong token | 401 |

**DELETE /configure**

| Test | Setup | Expected |
|---|---|---|
| Valid deletion | confirm matches project name | 200, project and buckets gone from DB |
| Wrong confirm value | `confirm: "wrong"` | 400 |
| Missing confirm | no confirm field | 400 |
| Project not found | empty DB | 404 |
| Invalid API key | wrong token | 401 |

**DELETE /configure/buckets/:bucket_name**

| Test | Setup | Expected |
|---|---|---|
| Valid deletion, no files | empty bucket | 200, bucket gone |
| Bucket has verified uploads, no force | `force: false` | 400 |
| Bucket has verified uploads, force true | `force: true` | 200, bucket gone, uploads orphaned |
| Bucket not found | wrong name | 404 |
| Invalid API key | wrong token | 401 |

### Upload Endpoints — `internal/handlers/upload_test.go`

**POST /upload/presign**

| Test | Setup | Expected |
|---|---|---|
| Valid presign | configured bucket, valid mime and size | 201, upload_url and upload_id returned |
| MIME not in allowlist | `image/gif` against jpeg-only bucket | 422 |
| File size exceeds bucket max | size over max_bytes | 422 |
| File size would exceed quota | used_bytes close to quota_bytes | 429 |
| TTL override applied | `ttl_seconds: 600` | 201, expires_at reflects 600s TTL |
| TTL override exceeds max | `ttl_seconds: 999999` | 422 |
| TTL falls back to bucket default | no ttl_seconds in request | 201, expires_at reflects bucket presign_ttl_seconds |
| TTL falls back to global default | no ttl_seconds, no bucket TTL | 201, expires_at reflects global default |
| Bucket not found | wrong bucket_name | 404 |
| Missing filename | no filename field | 400 |
| Missing mime_type | no mime_type field | 400 |
| Missing file_size | no file_size field | 400 |
| Invalid API key | wrong token | 401 |
| Upload record created | valid request | upload in DB with status pending |

**POST /upload/confirm**

The mock SeaweedFS server is configured per test to return controlled headObject responses and ranged GET byte sequences.

| Test | Mock SeaweedFS | Expected |
|---|---|---|
| Valid JPEG | returns jpeg magic bytes | 200, status verified, public_url populated (public bucket) |
| Valid JPEG private bucket | returns jpeg magic bytes | 200, status verified, public_url null |
| Magic bytes mismatch | returns PNG bytes for declared JPEG | 422, status rejected, object deleted from mock |
| Size exceeds bucket max on headObject | Content-Length over max_bytes | 422, status rejected, object deleted |
| MIME mismatch on headObject | Content-Type differs from declared | 422, status rejected |
| Object not found on SeaweedFS | headObject returns 404 | 422, status rejected |
| Quota deducted on success | verify quota headroom decreases | used_bytes incremented by size_bytes |
| Replay attack | call confirm twice on same upload_id | 409 on second call |
| Wrong project | upload_id belongs to different project | 404 |
| Upload not found | random upload_id | 404 |
| Expired upload | upload past expires_at | 409 or 404 depending on sweeper state |
| Invalid API key | wrong token | 401 |

### Files Endpoints — `internal/handlers/files_test.go`

**POST /files/:upload_id/presign-read**

| Test | Setup | Expected |
|---|---|---|
| Valid private bucket read | verified upload in private bucket | 200, read_url and expires_at returned |
| Public bucket rejected | verified upload in public bucket | 403 |
| TTL override applied | `ttl_seconds: 1800` | 200, expires_at reflects 1800s |
| TTL falls back to bucket read_ttl | no ttl_seconds | 200, expires_at reflects read_ttl_seconds |
| Upload not verified | pending status | 404 |
| Upload rejected | rejected status | 404 |
| Upload not found | random upload_id | 404 |
| Wrong project | upload belongs to different project | 404 |
| Invalid API key | wrong token | 401 |

**DELETE /files/:upload_id**

| Test | Setup | Expected |
|---|---|---|
| Valid deletion | verified upload | 200, object deleted from mock SeaweedFS, used_bytes decremented |
| Quota decrement | verified upload of 512000 bytes | used_bytes decremented by 512000 |
| Upload not verified | pending status | 404 |
| Upload already deleted | call delete twice | 404 on second call |
| Wrong project | upload belongs to different project | 404 |
| SeaweedFS delete fails | mock returns 500 | 500, upload record not marked deleted |
| Invalid API key | wrong token | 401 |

### Background Jobs — `internal/handlers/jobs_test.go`

**Expired upload sweeper**

| Test | Setup | Expected |
|---|---|---|
| Cleans expired pending upload | pending upload with expires_at in past | status set to rejected, reason is expired |
| Ignores current pending upload | pending upload with future expires_at | record unchanged |
| Ignores verified uploads | verified upload with past expires_at | record unchanged |
| Ignores rejected uploads | already rejected record | record unchanged |
| Deletes orphaned SeaweedFS object | expired upload, mock has the object | DELETE called on mock SeaweedFS |
| Handles missing SeaweedFS object | expired upload, mock returns 404 | sweeper continues without error |

**Webhook retry worker**

| Test | Setup | Expected |
|---|---|---|
| Retries failed delivery | webhook_delivery with succeeded=0, attempt=1 | second attempt made |
| Respects backoff | attempt=2, last delivery too recent | no retry until backoff elapses |
| Stops at 5 attempts | attempt=5, still failing | no further retries, marked permanently failed |
| Marks success | mock webhook returns 200 on retry | succeeded=1 in DB |
| Does not retry succeeded | succeeded=1 | no further calls to mock webhook |

### Health Endpoints — `internal/handlers/health_test.go`

| Test | Setup | Expected |
|---|---|---|
| Shallow health | process running | 200, status ok |
| Deep health both up | DB and mock SeaweedFS responding | 200, both checks ok with latency_ms |
| Deep health SeaweedFS down | mock SeaweedFS closed | 503, seaweedfs check shows error |
| Deep health SQLite down | DB connection closed | 503, sqlite check shows error |
| No auth required | no Authorization header | 200 — health endpoints are unauthenticated |

---

## End-to-End Tests

E2E tests are tagged `e2e` and excluded from `go test ./...`. They require Docker Compose to be running with real SeaweedFS.

```bash
docker compose up -d
go test ./tests/e2e/... -tags e2e -v
```

These tests use actual file bytes — real JPEGs, real PDFs, real MP4 fragments — not synthetic data. They test the full path from presign through browser PUT through confirm through webhook delivery.

### Full Upload Flow — `tests/e2e/upload_test.go`

| Test | File | Bucket | Expected |
|---|---|---|---|
| JPEG upload public bucket | valid 200KB JPEG | mediavault-avatars (public) | verified, public_url returned in webhook |
| PNG upload public bucket | valid 1MB PNG | mediavault-post-images (public) | verified, public_url permanent |
| PDF upload private bucket | valid 500KB PDF | mediavault-contracts (private) | verified, public_url null in webhook, presign-read returns working URL |
| MP4 upload private bucket | valid 5MB MP4 fragment | mediavault-full-videos (private) | verified |
| Tampered JPEG | PNG bytes with .jpg extension | mediavault-avatars (public) | rejected, object deleted from SeaweedFS |
| File exceeds max_bytes | 6MB file against 5MB bucket | mediavault-avatars (public) | presign rejected with 422 |
| MIME not allowed | image/gif against jpeg-only bucket | mediavault-avatars (public) | presign rejected with 422 |

### Private Read Flow — `tests/e2e/private_read_test.go`

| Test | Expected |
|---|---|
| Presign-read returns working URL | GET to read_url returns file bytes |
| Expired read URL rejected | SeaweedFS returns 403 after TTL |
| Public bucket presign-read rejected | 403 from OpenStore |

### Deletion Flow — `tests/e2e/delete_test.go`

| Test | Expected |
|---|---|
| Delete verified file | 200, file no longer accessible on SeaweedFS |
| Quota decremented after delete | GET /configure shows reduced used_bytes |
| Delete already deleted file | 404 |

### Configure Lifecycle — `tests/e2e/configure_test.go`

| Test | Expected |
|---|---|
| POST then GET returns identical config | all fields round-trip correctly |
| PUT adds bucket, GET reflects it | new bucket present |
| PATCH updates bucket TTL, presign reflects new TTL | expires_at uses updated TTL |
| DELETE project, all endpoints return 404 | project fully removed |

### Concurrent Upload Stress — `tests/e2e/concurrent_test.go`

Fires 50 concurrent presign requests then confirms all 50 uploads. Verifies:

- All 50 uploads reach verified status
- `used_bytes` on the project equals the sum of all 50 file sizes exactly — no double-counting or missed increments under concurrent SQLite writes
- No upload record reaches verified status more than once

---

## Test File Fixtures

Real file bytes are used in E2E tests. Fixtures live in `tests/fixtures/`.

```
tests/fixtures/
  valid.jpg         200KB valid JPEG
  valid.png         1MB valid PNG
  valid.webp        300KB valid WebP
  valid.pdf         500KB valid PDF
  valid.mp4         5MB valid MP4 fragment
  valid.mp3         1MB valid MP3
  tampered.jpg      PNG file bytes with .jpg extension
  empty.jpg         0-byte file
  truncated.jpg     First 3 bytes of a JPEG only
```

Unit and integration tests that need real bytes import from this package rather than generating synthetic data, so the same fixtures cover all three test categories.

---

## Coverage Targets

| Package | Target |
|---|---|
| `internal/security` | 100% — every MIME type and tamper case |
| `internal/quota` | 100% |
| `internal/webhook` | 95% |
| `internal/handlers` | 90% |
| `internal/seaweedfs` | 85% |
| `internal/db` | 80% |

Run coverage report:

```bash
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```
