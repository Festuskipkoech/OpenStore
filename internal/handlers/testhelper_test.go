package handlers_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"openstore/internal/config"
	"openstore/internal/db"
	"openstore/internal/handlers"
	"openstore/internal/middleware"
	"openstore/internal/security"
)

const testAPIKey = "test-api-key"
const testPublicBaseURL = "http://localhost:8080"

// mockSeaweed satisfies handlers.Pinger and handlers.SeaweedClient.
// Stores objects in memory, tracks delete calls, and can simulate failures.
type mockSeaweed struct {
	mu sync.Mutex
	objects map[string][]byte
	deleteCalls []string
	pingErr error
	writeErr error
}

func newMockSeaweed() *mockSeaweed {
	return &mockSeaweed{objects: map[string][]byte{}}
}

func (m *mockSeaweed) Ping(_ context.Context) error { return m.pingErr }

func (m *mockSeaweed) WriteObject(_ context.Context, key, _ string, r io.Reader) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.objects[key] = data
	m.mu.Unlock()
	return nil
}

func (m *mockSeaweed) ReadObject(_ context.Context, key string, w io.Writer) error {
	m.mu.Lock()
	data, ok := m.objects[key]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("object not found: %s", key)
	}
	_, err := w.Write(data)
	return err
}

func (m *mockSeaweed) DeleteObject(_ context.Context, key string) error {
	m.mu.Lock()
	delete(m.objects, key)
	m.deleteCalls = append(m.deleteCalls, key)
	m.mu.Unlock()
	return nil
}

func (m *mockSeaweed) PublicURL(key string) string {
	return testPublicBaseURL + "/objects/" + key
}

func (m *mockSeaweed) deleteCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.deleteCalls)
}

func (m *mockSeaweed) seed(key string, data []byte) {
	m.mu.Lock()
	m.objects[key] = data
	m.mu.Unlock()
}

// testEnv holds everything a handler test needs.
type testEnv struct {
	db  *sql.DB
	seaweed *mockSeaweed
	tokenizer *security.Tokenizer
	router http.Handler
}

// newTestEnv builds an env with only configure and health routes.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	database := openTestDB(t)
	seaweed := newMockSeaweed()
	return &testEnv{
		db: database,
		seaweed: seaweed,
		router: buildRouter(t, database, seaweed),
	}
}

// newFullTestEnv builds an env with all routes mounted.
func newFullTestEnv(t *testing.T) *testEnv {
	t.Helper()
	database := openTestDB(t)
	seaweed := newMockSeaweed()
	tokenizer := security.NewTokenizer(testAPIKey)
	return &testEnv{
		db: database,
		seaweed: seaweed,
		tokenizer: tokenizer,
		router: buildFullRouter(t, database, seaweed, tokenizer),
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func buildRouter(t *testing.T, database *sql.DB, seaweed *mockSeaweed) http.Handler {
	t.Helper()

	healthHandler := handlers.NewHealthHandler(database, seaweed)
	configureHandler := handlers.NewConfigureHandler(database)

	r := chi.NewRouter()
	r.Use(chimiddleware.ClientIPFromHeader("X-Real-IP"))
	r.Use(middleware.Recovery)
	r.Use(middleware.Logger)

	r.Get("/health", healthHandler.Shallow)
	r.Get("/health/deep", healthHandler.Deep)

	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(testAPIKey))

		r.Post("/configure", configureHandler.Create)
		r.Get("/configure", configureHandler.Get)
		r.Put("/configure", configureHandler.Update)
		r.Patch("/configure/buckets/{bucketName}", configureHandler.PatchBucket)
		r.Delete("/configure", configureHandler.Delete)
		r.Delete("/configure/buckets/{bucketName}", configureHandler.DeleteBucket)
	})

	return r
}

func buildFullRouter(t *testing.T, database *sql.DB, seaweed *mockSeaweed, tokenizer *security.Tokenizer) http.Handler {
	t.Helper()

	cfg := &config.Config{
		ClamAVEnabled:     false,
		ClamAVURL:         "",
		PresignTTLDefault: 300,
		PresignTTLMax:     86400,
		ReadTTLDefault:    900,
		PublicBaseURL:     testPublicBaseURL,
	}

	healthHandler := handlers.NewHealthHandler(database, seaweed)
	configureHandler := handlers.NewConfigureHandler(database)
	uploadHandler := handlers.NewUploadHandler(database, seaweed, tokenizer, cfg)
	filesHandler := handlers.NewFilesHandler(database, seaweed, tokenizer, cfg)

	r := chi.NewRouter()
	r.Use(chimiddleware.ClientIPFromHeader("X-Real-IP"))
	r.Use(middleware.Recovery)
	r.Use(middleware.Logger)

	r.Get("/health", healthHandler.Shallow)
	r.Get("/health/deep", healthHandler.Deep)

	r.Put("/upload/{uploadID}", uploadHandler.Stream)
	r.Get("/files/{uploadID}", filesHandler.ReadFile)

	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(testAPIKey))

		r.Post("/configure", configureHandler.Create)
		r.Get("/configure", configureHandler.Get)
		r.Put("/configure", configureHandler.Update)
		r.Patch("/configure/buckets/{bucketName}", configureHandler.PatchBucket)
		r.Delete("/configure", configureHandler.Delete)
		r.Delete("/configure/buckets/{bucketName}", configureHandler.DeleteBucket)

		r.Post("/upload/presign", uploadHandler.Presign)
		r.Get("/uploads/{uploadID}", filesHandler.GetUploadStatus)
		r.Post("/files/{uploadID}/read-presign", filesHandler.PresignRead)
		r.Delete("/files/{uploadID}", filesHandler.DeleteFile)
	})

	return r
}

func doRequest(t *testing.T, router http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode request body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func doRequestNoAuth(t *testing.T, router http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode request body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func doRequestWithKey(t *testing.T, router http.Handler, method, path, key string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode request body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func doRawRequest(t *testing.T, router http.Handler, method, path, contentType string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return result
}

func assertStatus(t *testing.T, w *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if w.Code != expected {
		t.Errorf("expected status %d, got %d — body: %s", expected, w.Code, w.Body.String())
	}
}

func assertErrorCode(t *testing.T, w *httptest.ResponseRecorder, code string) {
	t.Helper()
	body := decodeBody(t, w)
	got, ok := body["code"].(string)
	if !ok {
		t.Fatalf("response has no code field: %s", w.Body.String())
	}
	if got != code {
		t.Errorf("expected error code %q, got %q", code, got)
	}
}

func projectExistsInDB(t *testing.T, db *sql.DB, projectID string) bool {
	t.Helper()
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM projects WHERE id = ?", projectID).Scan(&count)
	if err != nil {
		t.Fatalf("query project: %v", err)
	}
	return count > 0
}

func bucketExistsInDB(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM buckets WHERE name = ?", name).Scan(&count)
	if err != nil {
		t.Fatalf("query bucket: %v", err)
	}
	return count > 0
}

func validProjectBody() map[string]any {
	return map[string]any{
		"project": map[string]any{
			"name": "testproject",
			"webhook_url": "https://example.com/webhook",
			"webhook_secret": "supersecret",
			"allowed_origins": []string{"https://example.com"},
			"quota_bytes": 10737418240,
		},
		"buckets": []map[string]any{
			{
				"name": "testproject-images",
				"media_class": "images",
				"allowed_mime": []string{"image/jpeg", "image/png"},
				"max_bytes": 5242880,
				"presign_ttl_seconds": 300,
				"access": "public",
			},
		},
	}
}

// insertTestProject inserts a project and bucket directly and returns their IDs.
func insertTestProject(t *testing.T, database *sql.DB, access string) (projectID, bucketID string) {
	t.Helper()
	projectID = "proj-" + t.Name()
	bucketID = "buck-" + t.Name()

	_, err := database.Exec(`
		INSERT INTO projects (id, name, api_key_hash, webhook_url, webhook_secret, allowed_origins, quota_bytes, used_bytes)
		VALUES (?, 'testproject', '', 'http://webhook.test', 'secret', '["http://localhost:3000"]', 10737418240, 0)`,
		projectID,
	)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}

	_, err = database.Exec(`
		INSERT INTO buckets (id, project_id, name, media_class, allowed_mime, max_bytes, presign_ttl_seconds, read_ttl_seconds, access)
		VALUES (?, ?, 'test-bucket', 'images', '["image/jpeg"]', 5242880, 300, 900, ?)`,
		bucketID, projectID, access,
	)
	if err != nil {
		t.Fatalf("insert bucket: %v", err)
	}

	return projectID, bucketID
}

// insertVerifiedUpload inserts a verified upload record directly.
func insertVerifiedUpload(t *testing.T, database *sql.DB, projectID, bucketID, objectKey string, sizeBytes int64, access string) string {
	t.Helper()
	uploadID := "upload-" + t.Name()
	now := time.Now().UTC()

	var publicURL *string
	if access == "public" {
		u := testPublicBaseURL + "/objects/" + objectKey
		publicURL = &u
	}

	_, err := database.Exec(`
		INSERT INTO uploads (id, project_id, bucket_id, object_key, original_name, content_type,
		size_bytes, public_url, status, verified_at, expires_at)
		VALUES (?, ?, ?, ?, 'test.jpg', 'image/jpeg', ?, ?, 'verified', ?, datetime('now', '+1 hour'))`,
		uploadID, projectID, bucketID, objectKey, sizeBytes, publicURL, now,
	)
	if err != nil {
		t.Fatalf("insert verified upload: %v", err)
	}

	_, err = database.Exec(`UPDATE projects SET used_bytes = used_bytes + ? WHERE id = ?`, sizeBytes, projectID)
	if err != nil {
		t.Fatalf("update used_bytes: %v", err)
	}

	return uploadID
}

// insertPendingUpload inserts a pending upload with a controlled expiry datetime string.
func insertPendingUpload(t *testing.T, database *sql.DB, projectID, bucketID, objectKey string, expiresAt time.Time) string {
    t.Helper()
    uploadID := "pending-" + t.Name()
	_, err := database.Exec(`
		INSERT INTO uploads (id, project_id, bucket_id, object_key, original_name, content_type,
		size_bytes, status, expires_at)
		VALUES (?, ?, ?, ?, 'test.jpg', 'image/jpeg', 1024, 'pending', ?)`,
		uploadID, projectID, bucketID, objectKey, expiresAt.UTC().Format("2006-01-02 15:04:05"),
	)
    if err != nil {
        t.Fatalf("insert pending upload: %v", err)
    }
    return uploadID
}
func insertTestProjectWithMIME(t *testing.T, database *sql.DB, access, mediaClass, allowedMIME, contentType string) (projectID, bucketID string) {
	t.Helper()
	projectID = "proj-" + t.Name()
	bucketID = "buck-" + t.Name()

	_, err := database.Exec(`
		INSERT INTO projects (id, name, api_key_hash, webhook_url, webhook_secret, allowed_origins, quota_bytes, used_bytes)
		VALUES (?, 'testproject', '', 'http://webhook.test', 'secret', '["http://localhost:3000"]', 10737418240, 0)`,
		projectID,
	)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}

	_, err = database.Exec(`
		INSERT INTO buckets (id, project_id, name, media_class, allowed_mime, max_bytes, presign_ttl_seconds, read_ttl_seconds, access)
		VALUES (?, ?, 'test-bucket', ?, ?, 5242880, 300, 900, ?)`,
		bucketID, projectID, mediaClass, allowedMIME, access,
	)
	if err != nil {
		t.Fatalf("insert bucket: %v", err)
	}

	return projectID, bucketID
}

func insertPendingUploadForStreamWithMIME(t *testing.T, env *testEnv, access, mediaClass, mimeType string) (uploadID, objectKey, token string) {
	t.Helper()
	projectID, bucketID := insertTestProjectWithMIME(t, env.db, access, mediaClass, `["`+mimeType+`"]`, mimeType)

	uploadID = "stream-mime-" + t.Name()
	ext := mimeExt(mimeType)
	objectKey = mediaClass + "/" + projectID + "/2026/08/" + uploadID + ext

	_, err := env.db.Exec(`
		INSERT INTO uploads (id, project_id, bucket_id, object_key, original_name, content_type,
		size_bytes, status, expires_at)
		VALUES (?, ?, ?, ?, 'test'+?, ?, 1024, 'pending', datetime('now', '+5 minutes'))`,
		uploadID, projectID, bucketID, objectKey, ext, mimeType,
	)
	if err != nil {
		t.Fatalf("insert pending upload: %v", err)
	}

	token = signUploadToken(t, env.tokenizer, uploadID, "test-bucket", mimeType, 1024, 5*time.Minute)
	return uploadID, objectKey, token
}

func mimeExt(mimeType string) string {
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "audio/mpeg":
		return ".mp3"
	case "audio/wav":
		return ".wav"
	case "audio/ogg":
		return ".ogg"
	case "audio/flac":
		return ".flac"
	case "application/pdf":
		return ".pdf"
	default:
		return ".bin"
	}
}
// signUploadToken signs a valid upload token.
func signUploadToken(t *testing.T, tokenizer *security.Tokenizer, uploadID, bucketName, mimeType string, fileSize int64, ttl time.Duration) string {
	t.Helper()
	return tokenizer.Sign(security.TokenClaims{
		UploadID: uploadID,
		BucketName: bucketName,
		MIMEType: mimeType,
		FileSize: fileSize,
		ExpiresAt: time.Now().UTC().Add(ttl),
	})
}