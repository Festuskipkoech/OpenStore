package handlers_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"openstore/internal/jobs"
	"openstore/internal/webhook"
)

// sweeper tests
func TestSweeper_CleansExpiredPending(t *testing.T) {
	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "public")
	objectKey := "images/test/expired.jpg"
	env.seaweed.seed(objectKey, []byte("data"))

	// expires_at in the past — should be swept
	uploadID := insertPendingUpload(t, env.db, projectID, bucketID, objectKey, time.Now().Add(-1*time.Minute))

	jobs.Sweep(context.Background(), env.db, env.seaweed)

	var status string
	env.db.QueryRow("SELECT status FROM uploads WHERE id = ?", uploadID).Scan(&status)
	if status != "rejected" {
		t.Errorf("expected status rejected after sweep, got %s", status)
	}

	var reason string
	env.db.QueryRow("SELECT rejection_reason FROM uploads WHERE id = ?", uploadID).Scan(&reason)
	if reason != "expired" {
		t.Errorf("expected rejection_reason expired, got %s", reason)
	}
}

func TestSweeper_IgnoresCurrentPending(t *testing.T) {
	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "public")

	objectKey := "images/test/verified.jpg"
	env.seaweed.seed(objectKey, []byte("data"))

	// expires_at in the future — must not be touched
	uploadID := insertPendingUpload(t, env.db, projectID, bucketID, objectKey, time.Now().Add(1*time.Hour))

	jobs.Sweep(context.Background(), env.db, env.seaweed)

	var status string
	env.db.QueryRow("SELECT status FROM uploads WHERE id = ?", uploadID).Scan(&status)
	if status != "pending" {
		t.Errorf("expected status pending, got %s", status)
	}
}

func TestSweeper_IgnoresVerified(t *testing.T) {
	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "public")
	objectKey := "images/test/verified.jpg"
	env.seaweed.seed(objectKey, []byte("data"))
	uploadID := insertVerifiedUpload(t, env.db, projectID, bucketID, objectKey, 1024, "public")

	jobs.Sweep(context.Background(), env.db, env.seaweed)

	var status string
	env.db.QueryRow("SELECT status FROM uploads WHERE id = ?", uploadID).Scan(&status)
	if status != "verified" {
		t.Errorf("expected status verified to be unchanged, got %s", status)
	}
}

func TestSweeper_DeletesSeaweedObject(t *testing.T) {
	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "public")
	objectKey := "images/test/del-sweep.jpg"
	env.seaweed.seed(objectKey, []byte("data"))

	insertPendingUpload(t, env.db, projectID, bucketID, objectKey, time.Now().Add(-1*time.Minute))

	jobs.Sweep(context.Background(), env.db, env.seaweed)

	if env.seaweed.deleteCallCount() == 0 {
		t.Error("expected DeleteObject to be called for expired upload")
	}
}

func TestSweeper_HandlesSeaweedNotFound(t *testing.T) {
	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "public")

	objectKey := "images/test/del-sweep.jpg"
	env.seaweed.seed(objectKey, []byte("data"))

	// object not seeded — DeleteObject returns not-found, sweep must still complete
	uploadID := insertPendingUpload(t, env.db, projectID, bucketID, objectKey, time.Now().Add(-1*time.Minute))

	jobs.Sweep(context.Background(), env.db, env.seaweed)

	var status string
	env.db.QueryRow("SELECT status FROM uploads WHERE id = ?", uploadID).Scan(&status)
	if status != "rejected" {
		t.Errorf("expected status rejected even when seaweed object missing, got %s", status)
	}
}

// retry worker tests
func insertDelivery(t *testing.T, env *testEnv, uploadID, projectID, url string, attempt int, succeeded bool, deliveredAt time.Time) string {	t.Helper()
	deliveryID := "del-" + t.Name()
	s := 0
	if succeeded {
		s = 1
	}
	_, err := env.db.Exec(`
		INSERT INTO webhook_deliveries (id, upload_id, project_id, url, attempt, succeeded, delivered_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		deliveryID, uploadID, projectID, url, attempt, s, deliveredAt.UTC().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		t.Fatalf("insert delivery: %v", err)
	}
	return deliveryID
}

func TestRetry_RetriesFailedDelivery(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "public")
	uploadID := insertVerifiedUpload(t, env.db, projectID, bucketID, "images/test/retry.jpg", 1024, "public")

	// attempt=1, old enough to retry (>10 seconds ago)
	insertDelivery(t, env, uploadID, projectID, server.URL, 1, false, time.Now().Add(-30*time.Second))

	// update project webhook_url to point to test server
	env.db.Exec("UPDATE projects SET webhook_url = ? WHERE id = ?", server.URL, projectID)

	webhook.RetryPending(env.db)

	if attempts == 0 {
		t.Error("expected retry worker to call webhook server")
	}
}

func TestRetry_RespectsBackoff(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "public")
	uploadID := insertVerifiedUpload(t, env.db, projectID, bucketID, "images/test/backoff.jpg", 1024, "public")

	// attempt=1 but only 5 seconds ago — backoff requires 10 seconds
	insertDelivery(t, env, uploadID, projectID, server.URL, 1, false, time.Now().Add(-5*time.Second))
	env.db.Exec("UPDATE projects SET webhook_url = ? WHERE id = ?", server.URL, projectID)

	webhook.RetryPending(env.db)

	if attempts > 0 {
		t.Error("expected retry to be skipped due to backoff")
	}
}

func TestRetry_StopsAtFiveAttempts(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "public")
	uploadID := insertVerifiedUpload(t, env.db, projectID, bucketID, "images/test/maxattempt.jpg", 1024, "public")

	// attempt=5 — must not retry
	insertDelivery(t, env, uploadID, projectID, server.URL, 5, false, time.Now().Add(-1*time.Hour))
	env.db.Exec("UPDATE projects SET webhook_url = ? WHERE id = ?", server.URL, projectID)

	webhook.RetryPending(env.db)

	if attempts > 0 {
		t.Error("expected no retry after 5 attempts")
	}
}

func TestRetry_MarksSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "public")
	uploadID := insertVerifiedUpload(t, env.db, projectID, bucketID, "images/test/success.jpg", 1024, "public")

	deliveryID := insertDelivery(t, env, uploadID, projectID, server.URL, 1, false, time.Now().Add(-30*time.Second))
	env.db.Exec("UPDATE projects SET webhook_url = ? WHERE id = ?", server.URL, projectID)

	webhook.RetryPending(env.db)

	var succeeded int
	env.db.QueryRow("SELECT succeeded FROM webhook_deliveries WHERE id = ?", deliveryID).Scan(&succeeded)
	if succeeded != 1 {
		t.Error("expected delivery to be marked succeeded after successful retry")
	}
}

func TestRetry_DoesNotRetrySucceeded(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "public")
	uploadID := insertVerifiedUpload(t, env.db, projectID, bucketID, "images/test/alreadydone.jpg", 1024, "public")

	// succeeded=true — must not be retried
	insertDelivery(t, env, uploadID, projectID, server.URL, 5, false, time.Now().Add(-1*time.Hour))
	env.db.Exec("UPDATE projects SET webhook_url = ? WHERE id = ?", server.URL, projectID)

	webhook.RetryPending(env.db)

	if attempts > 0 {
		t.Error("expected already-succeeded delivery not to be retried")
	}
}

// verify the webhook signature on delivery
func TestRetry_SignaturePresent(t *testing.T) {
	var receivedSig string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSig = r.Header.Get("X-OpenStore-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "public")
	uploadID := insertVerifiedUpload(t, env.db, projectID, bucketID, "images/test/sig.jpg", 1024, "public")

	insertDelivery(t, env, uploadID, projectID, server.URL, 1, false, time.Now().Add(-30*time.Second))
	env.db.Exec("UPDATE projects SET webhook_url = ? WHERE id = ?", server.URL, projectID)

	webhook.RetryPending(env.db)

	if receivedSig == "" {
		t.Error("expected X-OpenStore-Signature header on webhook delivery")
	}
	if len(receivedSig) < len("hmac-sha256=") {
		t.Errorf("expected hmac-sha256= prefix, got: %s", receivedSig)
	}
}

// ensure json body is valid on delivery
func TestRetry_PayloadIsValidJSON(t *testing.T) {
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "public")
	uploadID := insertVerifiedUpload(t, env.db, projectID, bucketID, "images/test/payload.jpg", 1024, "public")

	insertDelivery(t, env, uploadID, projectID, server.URL, 1, false, time.Now().Add(-30*time.Second))
	env.db.Exec("UPDATE projects SET webhook_url = ? WHERE id = ?", server.URL, projectID)

	webhook.RetryPending(env.db)

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Errorf("expected valid JSON payload, got error: %v", err)
	}
	if parsed["event"] == nil {
		t.Error("expected event field in webhook payload")
	}
}