# OpenStore — Remaining Tests Guide

This guide covers every test file not yet written. For each file: what it tests,
what dependencies it needs, how to set up the test environment, and the exact
cases to implement.

---

## 1. `internal/handlers/health_test.go`

**What it tests:** `GET /health` and `GET /health/deep`.

**Dependencies:**
- Real temp file DB via `openTestDB(t)` from `testhelper_test.go`
- A mock SeaweedFS client that can be toggled healthy/unhealthy
- Chi router with health routes mounted — no auth middleware on these routes

**Mock SeaweedFS for health:**
```go
type mockHealthSeaweed struct {
    healthy bool
}

func (m *mockHealthSeaweed) Ping(_ context.Context) error {
    if !m.healthy {
        return errors.New("connection refused")
    }
    return nil
}
```

**Setup pattern:**
```go
func newHealthEnv(t *testing.T, seaweedHealthy bool) *healthEnv {
    database := openTestDB(t)
    seaweed := &mockHealthSeaweed{healthy: seaweedHealthy}
    handler := handlers.NewHealthHandler(database, seaweed)
    r := chi.NewRouter()
    r.Get("/health", handler.Shallow)
    r.Get("/health/deep", handler.Deep)
    return &healthEnv{db: database, router: r}
}
```

**Cases to implement:**

| Test | Setup | Expected |
|---|---|---|
| `TestHealth_Shallow` | process running | 200, `{"status":"ok"}` |
| `TestHealth_Deep_BothUp` | DB and seaweed healthy | 200, both checks ok, latency_ms present |
| `TestHealth_Deep_SeaweedDown` | seaweed.healthy = false | 503, seaweedfs check shows error |
| `TestHealth_Deep_SQLiteDown` | close DB before request | 503, sqlite check shows error |
| `TestHealth_NoAuthRequired` | no Authorization header | 200 on both endpoints |

**Note on SQLite down test:** call `database.Close()` before firing the request,
then assert 503. The handler must not panic.

---

## 2. `internal/handlers/upload_stream_test.go`

**What it tests:** `PUT /upload/{uploadID}` — the full ten-step verification chain.

**Dependencies:**
- Real temp file DB
- Mock SeaweedFS that tracks calls — write, read, delete
- `security.NewTokenizer` for generating valid tokens
- `config.Config` with `ClamAVEnabled: false` for tests (no real ClamAV)
- Chi router with the stream route mounted outside auth group

**Mock SeaweedFS for stream:**
```go
type mockStreamSeaweed struct {
    written     map[string][]byte
    deleteCalls []string
    readFunc    func(key string, w io.Writer) error
}

func (m *mockStreamSeaweed) WriteObject(_ context.Context, key, _ string, r io.Reader) error {
    data, _ := io.ReadAll(r)
    m.written[key] = data
    return nil
}
func (m *mockStreamSeaweed) DeleteObject(_ context.Context, key string) error {
    m.deleteCalls = append(m.deleteCalls, key)
    return nil
}
func (m *mockStreamSeaweed) ReadObject(_ context.Context, key string, w io.Writer) error {
    if m.readFunc != nil {
        return m.readFunc(key, w)
    }
    if data, ok := m.written[key]; ok {
        w.Write(data)
    }
    return nil
}
func (m *mockStreamSeaweed) PublicURL(key string) string {
    return "http://cdn.example.com/" + key
}
```

**Setup pattern:**
```go
func newStreamEnv(t *testing.T) *streamEnv {
    database := openTestDB(t)
    seaweed := &mockStreamSeaweed{written: map[string][]byte{}}
    tokenizer := security.NewTokenizer("test-api-key")
    cfg := &config.Config{ClamAVEnabled: false}
    handler := handlers.NewUploadHandler(database, seaweed, tokenizer, cfg)
    r := chi.NewRouter()
    r.Put("/upload/{uploadID}", handler.Stream)
    return &streamEnv{db: database, seaweed: seaweed, tokenizer: tokenizer, router: r}
}
```

**Helper — create project, bucket, upload, and token:**
```go
func createPendingUpload(t *testing.T, env *streamEnv, access string) (uploadID, token, objectKey string) {
    // insert project, bucket, upload directly into DB
    // sign token with env.tokenizer
    // return all three for use in PUT request
}
```

**Cases to implement:**

| Test | Setup | Expected |
|---|---|---|
| `TestStream_ValidJPEG_PublicBucket` | valid JPEG magic bytes | 200, status verified, public_url populated |
| `TestStream_ValidJPEG_PrivateBucket` | valid JPEG, private bucket | 200, status verified, public_url null |
| `TestStream_MissingToken` | no token query param | 401 |
| `TestStream_ExpiredToken` | token with past expires_at | 401 |
| `TestStream_MagicBytesMismatch` | PNG bytes declared as JPEG | 422, status rejected, DeleteObject called once |
| `TestStream_FileTooSmall` | body smaller than HeaderSize | 422, status rejected |
| `TestStream_ContentTypeMismatch` | Content-Type differs from token | 400 |
| `TestStream_ReplayAttack` | PUT same upload_id twice | 409 on second call |
| `TestStream_QuotaDeducted` | successful upload | used_bytes incremented by file size |
| `TestStream_SizeExceedsBucketMax` | body larger than max_bytes | 413, DeleteObject called |

**Note:** ClamAV and govips/pdfcpu are disabled in tests via `ClamAVEnabled: false`
and the mock SeaweedFS returning controlled bytes for `ReadObject`. Deep inspection
is effectively bypassed for image types by having the mock return valid sanitisable bytes.

---

## 3. `internal/handlers/files_test.go`

**What it tests:** `GET /uploads/{uploadID}`, `GET /files/{uploadID}`,
`POST /files/{uploadID}/read-presign`, `DELETE /files/{uploadID}`.

**Dependencies:**
- Real temp file DB
- Mock SeaweedFS that serves controlled file bytes and tracks delete calls
- `security.NewTokenizer` for read token generation and verification
- `config.Config` with `PublicBaseURL`
- Chi router with all file routes mounted, with auth middleware for protected routes

**Mock SeaweedFS for files:**
```go
type mockFilesSeaweed struct {
    objects     map[string][]byte
    deleteCalls []string
}

func (m *mockFilesSeaweed) ReadObject(_ context.Context, key string, w io.Writer) error {
    if data, ok := m.objects[key]; ok {
        w.Write(data)
        return nil
    }
    return fmt.Errorf("object not found: %s", key)
}
func (m *mockFilesSeaweed) DeleteObject(_ context.Context, key string) error {
    delete(m.objects, key)
    m.deleteCalls = append(m.deleteCalls, key)
    return nil
}
func (m *mockFilesSeaweed) WriteObject(_ context.Context, key, _ string, r io.Reader) error {
    data, _ := io.ReadAll(r)
    m.objects[key] = data
    return nil
}
func (m *mockFilesSeaweed) PublicURL(key string) string {
    return "http://cdn.example.com/" + key
}
```

**Cases to implement:**

| Test | Setup | Expected |
|---|---|---|
| `TestGetUploadStatus_Verified` | verified upload | 200, status verified |
| `TestGetUploadStatus_Pending` | pending upload | 200, status pending, public_url null |
| `TestGetUploadStatus_NotFound` | random upload_id | 404 |
| `TestReadFile_PublicBucket` | verified public upload, bytes in mock | 200, correct Content-Type, bytes match |
| `TestReadFile_PrivateBucket_ValidToken` | verified private upload, valid read token | 200, bytes returned |
| `TestReadFile_PrivateBucket_NoToken` | verified private upload, no token | 401 |
| `TestReadFile_PrivateBucket_ExpiredToken` | expired read token | 401 |
| `TestReadFile_PrivateBucket_UploadTokenRejected` | upload token used as read token | 401 |
| `TestReadFile_PendingUpload` | pending status | 404 |
| `TestPresignRead_ValidPrivateBucket` | verified private upload | 200, read_url and expires_at returned |
| `TestPresignRead_PublicBucketRejected` | verified public upload | 400 |
| `TestPresignRead_NotFound` | random upload_id | 404 |
| `TestDeleteFile_Valid` | verified upload | 200, DeleteObject called, used_bytes decremented |
| `TestDeleteFile_QuotaDecrement` | verified upload, 512000 bytes | used_bytes decremented by 512000 exactly |
| `TestDeleteFile_AlreadyDeleted` | call delete twice | 404 on second call |
| `TestDeleteFile_PendingUpload` | pending status | 404 |

---

## 4. `internal/handlers/jobs_test.go`

**What it tests:** The sweeper and retry worker logic directly — not via HTTP.

**Dependencies:**
- Real temp file DB
- Mock SeaweedFS tracking delete calls
- Direct calls to `jobs.sweep` and `webhook.retryPending` — these need to be
  exported or tested via a thin exported wrapper
- `httptest.NewServer` as a mock webhook receiver for retry worker tests

**Note on exporting:** `sweep` and `retryPending` are currently unexported in
`internal/jobs/sweeper.go` and `internal/webhook/deliver.go`. You have two options:
- Export them: `Sweep(ctx, db, seaweed)` and `RetryPending(db)`
- Create exported test helpers in the same package using `_test.go` files in
  package `jobs` (not `jobs_test`) — this gives access to unexported functions

Recommended: export `Sweep` and `RetryPending` — they are meaningful operations
that operators may want to trigger manually in future.

**Sweeper cases:**

| Test | Setup | Expected |
|---|---|---|
| `TestSweeper_CleansExpiredPending` | pending upload, expires_at in past | status rejected, reason expired |
| `TestSweeper_IgnoresCurrentPending` | pending upload, expires_at in future | record unchanged |
| `TestSweeper_IgnoresVerified` | verified upload, expires_at in past | record unchanged |
| `TestSweeper_IgnoresRejected` | already rejected record | record unchanged |
| `TestSweeper_DeletesSeaweedObject` | expired upload, mock has object | DeleteObject called with correct key |
| `TestSweeper_HandlesSeaweedNotFound` | expired upload, mock returns not-found | sweeper completes without error |

**Retry worker cases:**

| Test | Setup | Expected |
|---|---|---|
| `TestRetry_RetriesFailedDelivery` | delivery succeeded=0, attempt=1, old timestamp | second attempt made to mock server |
| `TestRetry_RespectsBackoff` | attempt=2, delivered_at too recent | no retry fired |
| `TestRetry_StopsAtFiveAttempts` | attempt=5, succeeded=0 | no further call to mock server |
| `TestRetry_MarksSuccess` | mock webhook returns 200 on retry | succeeded=1 in DB |
| `TestRetry_DoesNotRetrySucceeded` | succeeded=1 | no call to mock server |

**Backoff schedule for tests** (matches `GetPendingDeliveries` query):
- attempt=1: retry after 10 seconds
- attempt=2: retry after 30 seconds
- attempt=3: retry after 2 minutes
- attempt=4: retry after 10 minutes

To test backoff without sleeping, insert records with controlled `delivered_at`
timestamps directly via SQL — set `delivered_at` to `datetime('now', '-5 seconds')`
for an attempt=1 record that should NOT yet retry.

---

## 5. E2E Tests — `tests/e2e/`

E2E tests require Docker Compose running with real SeaweedFS and ClamAV.
Tag all files with `//go:build e2e` and run with `go test ./tests/e2e/... -tags e2e`.

**Shared setup in `tests/e2e/helpers_test.go`:**
```go
//go:build e2e

package e2e_test

import (
    "net/http"
    "os"
    "testing"
)

func baseURL() string {
    if u := os.Getenv("OPENSTORE_BASE_URL"); u != "" {
        return u
    }
    return "http://localhost:8080"
}

func apiKey() string {
    return os.Getenv("OPENSTORE_API_KEY")
}

// TestMain checks the stack is healthy before running any E2E test.
// Skips all tests with a clear message if the stack is not up.
func TestMain(m *testing.M) {
    resp, err := http.Get(baseURL() + "/health/deep")
    if err != nil || resp.StatusCode != 200 {
        fmt.Println("skipping E2E tests: stack not running. Run: docker compose up -d")
        os.Exit(0)
    }
    os.Exit(m.Run())
}
```

**Fixtures in `tests/fixtures/`:**

| File | Content |
|---|---|
| `valid.jpg` | 200KB valid JPEG — real file bytes |
| `valid.png` | 1MB valid PNG |
| `valid.webp` | 300KB valid WebP |
| `valid.pdf` | 500KB clean PDF — no JS, no embedded executables |
| `valid.mp4` | 5MB valid MP4 fragment |
| `valid.mp3` | 1MB valid MP3 |
| `tampered.jpg` | PNG file bytes saved with .jpg extension |
| `empty.jpg` | 0-byte file |
| `truncated.jpg` | First 3 bytes of a JPEG only |

Generate tampered.jpg: copy valid.png bytes into a file named tampered.jpg.
OpenStore must reject it at magic byte verification.

---

### 5a. `tests/e2e/upload_test.go`

**What it tests:** Full upload flow from presign through browser PUT through webhook.

**Each test:**
1. POST /configure — create project and bucket
2. POST /upload/presign — get signed URL
3. PUT to upload URL with real file bytes
4. Assert response status and body
5. Assert webhook received (use httptest.NewServer as receiver)

| Test | File | Bucket access | Expected |
|---|---|---|---|
| `TestE2E_JPEG_PublicBucket` | valid.jpg | public | verified, public_url populated |
| `TestE2E_PDF_PrivateBucket` | valid.pdf | private | verified, public_url null |
| `TestE2E_TamperedJPEG` | tampered.jpg | public | 422 rejected |
| `TestE2E_FileTooLarge` | file > max_bytes | public | 422 at presign |
| `TestE2E_MIMENotAllowed` | image/gif against jpeg-only bucket | public | 422 at presign |

---

### 5b. `tests/e2e/private_read_test.go`

**What it tests:** Private file read flow with expiring signed URLs.

| Test | Expected |
|---|---|
| `TestE2E_PresignRead_ReturnsWorkingURL` | GET to read_url returns correct file bytes |
| `TestE2E_PresignRead_ExpiredToken` | wait for TTL then GET returns 401 |
| `TestE2E_PresignRead_PublicBucketRejected` | POST presign-read on public bucket returns 400 |

**Note on expiry test:** configure bucket with `read_ttl_seconds: 2`, presign,
wait 3 seconds, then GET. Assert 401.

---

### 5c. `tests/e2e/delete_test.go`

**What it tests:** File deletion and quota decrement.

| Test | Expected |
|---|---|
| `TestE2E_DeleteFile_Valid` | 200, file no longer accessible from SeaweedFS |
| `TestE2E_DeleteFile_QuotaDecrement` | GET /configure shows used_bytes reduced by file size |
| `TestE2E_DeleteFile_AlreadyDeleted` | 404 on second delete |

---

### 5d. `tests/e2e/configure_test.go`

**What it tests:** Full configure lifecycle against real DB.

| Test | Expected |
|---|---|
| `TestE2E_Configure_PostThenGet` | all fields round-trip correctly |
| `TestE2E_Configure_PutAddsBucket` | new bucket present after PUT |
| `TestE2E_Configure_PatchUpdatesTTL` | presign reflects updated TTL in expires_at |
| `TestE2E_Configure_DeleteProject` | all endpoints return 404 after DELETE |

---

### 5e. `tests/e2e/concurrent_test.go`

**What it tests:** 50 concurrent uploads with exact quota accounting.

**Approach:**
1. Create project with quota large enough for 50 uploads
2. Fire 50 goroutines simultaneously — each presigns and PUTs a known file size
3. Use `sync.WaitGroup` to wait for all
4. Assert all 50 uploads reach verified status
5. Assert `used_bytes` on the project equals exact sum of all 50 file sizes
6. Assert no upload_id appears in verified state more than once

```go
func TestE2E_ConcurrentUploads(t *testing.T) {
    // setup project
    var wg sync.WaitGroup
    results := make(chan string, 50) // upload IDs

    for i := 0; i < 50; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            uploadID := presignAndUpload(t, ...)
            results <- uploadID
        }()
    }

    wg.Wait()
    close(results)

    // assert all 50 verified, used_bytes exact
}
```

---

## Running Tests

```bash
# Unit tests only
go test ./internal/security/... ./internal/config/... ./internal/quota/... ./internal/middleware/... ./internal/webhook/... -v

# Integration tests only
go test ./internal/handlers/... -v

# All non-E2E
go test ./... -v

# E2E only (requires docker compose up -d)
go test ./tests/e2e/... -tags e2e -v

# Coverage report
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```
