package handlers_test

import (
	"net/http"
	"testing"
	"time"

	"openstore/internal/security"
)

func TestGetUploadStatus_Verified(t *testing.T) {
	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "public")
	uploadID := insertVerifiedUpload(t, env.db, projectID, bucketID, "images/test/obj.jpg", 204800, "public")

	w := doRequest(t, env.router, http.MethodGet, "/uploads/"+uploadID, nil)
	assertStatus(t, w, http.StatusOK)

	body := decodeBody(t, w)
	if body["status"] != "verified" {
		t.Errorf("expected status verified, got %v", body["status"])
	}
}

func TestGetUploadStatus_Pending(t *testing.T) {
	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "public")

	objectKey := "images/test/verified.jpg"
	env.seaweed.seed(objectKey, []byte("data"))
	uploadID := insertPendingUpload(t, env.db, projectID, bucketID, objectKey, time.Now().Add(1*time.Hour))

	w := doRequest(t, env.router, http.MethodGet, "/uploads/"+uploadID, nil)
	assertStatus(t, w, http.StatusOK)

	body := decodeBody(t, w)
	if body["status"] != "pending" {
		t.Errorf("expected status pending, got %v", body["status"])
	}
	if body["public_url"] != nil {
		t.Error("expected public_url to be null for pending upload")
	}
}

func TestGetUploadStatus_NotFound(t *testing.T) {
	env := newFullTestEnv(t)
	w := doRequest(t, env.router, http.MethodGet, "/uploads/nonexistent", nil)
	assertStatus(t, w, http.StatusNotFound)
	assertErrorCode(t, w, "not_found")
}

func TestReadFile_PublicBucket(t *testing.T) {
	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "public")
	objectKey := "images/test/public.jpg"
	fileData := []byte("fake-jpeg-content")
	env.seaweed.seed(objectKey, fileData)
	uploadID := insertVerifiedUpload(t, env.db, projectID, bucketID, objectKey, int64(len(fileData)), "public")

	w := doRawRequest(t, env.router, http.MethodGet, "/files/"+uploadID, "", nil, nil)
	assertStatus(t, w, http.StatusOK)

	if w.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("expected Content-Type image/jpeg, got %s", w.Header().Get("Content-Type"))
	}
	if w.Body.String() != string(fileData) {
		t.Error("response body does not match seeded file bytes")
	}
}

func TestReadFile_PrivateBucket_ValidToken(t *testing.T) {
	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "private")
	objectKey := "images/test/private.jpg"
	fileData := []byte("private-jpeg-content")
	env.seaweed.seed(objectKey, fileData)
	uploadID := insertVerifiedUpload(t, env.db, projectID, bucketID, objectKey, int64(len(fileData)), "private")

	token := env.tokenizer.SignRead(security.ReadTokenClaims{
		UploadID:  uploadID,
		ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
	})

	w := doRawRequest(t, env.router, http.MethodGet, "/files/"+uploadID+"?token="+token, "", nil, nil)
	assertStatus(t, w, http.StatusOK)
	if w.Body.String() != string(fileData) {
		t.Error("response body does not match seeded file bytes")
	}
}

func TestReadFile_PrivateBucket_NoToken(t *testing.T) {
	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "private")
	uploadID := insertVerifiedUpload(t, env.db, projectID, bucketID, "images/test/priv2.jpg", 1024, "private")

	w := doRawRequest(t, env.router, http.MethodGet, "/files/"+uploadID, "", nil, nil)
	assertStatus(t, w, http.StatusUnauthorized)
	assertErrorCode(t, w, "unauthorized")
}

func TestReadFile_PrivateBucket_ExpiredToken(t *testing.T) {
	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "private")
	uploadID := insertVerifiedUpload(t, env.db, projectID, bucketID, "images/test/priv3.jpg", 1024, "private")

	token := env.tokenizer.SignRead(security.ReadTokenClaims{
		UploadID:  uploadID,
		ExpiresAt: time.Now().UTC().Add(-1 * time.Minute),
	})

	w := doRawRequest(t, env.router, http.MethodGet, "/files/"+uploadID+"?token="+token, "", nil, nil)
	assertStatus(t, w, http.StatusUnauthorized)
	assertErrorCode(t, w, "unauthorized")
}

func TestReadFile_PrivateBucket_UploadTokenRejected(t *testing.T) {
	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "private")
	uploadID := insertVerifiedUpload(t, env.db, projectID, bucketID, "images/test/priv4.jpg", 1024, "private")

	// upload token — must not be accepted by the read endpoint
	uploadToken := signUploadToken(t, env.tokenizer, uploadID, "test-bucket", "image/jpeg", 1024, 5*time.Minute)

	w := doRawRequest(t, env.router, http.MethodGet, "/files/"+uploadID+"?token="+uploadToken, "", nil, nil)
	assertStatus(t, w, http.StatusUnauthorized)
	assertErrorCode(t, w, "unauthorized")
}

func TestReadFile_PendingUpload(t *testing.T) {
	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "public")

	objectKey := "images/test/verified.jpg"
	env.seaweed.seed(objectKey, []byte("data"))
	
	uploadID := insertPendingUpload(t, env.db, projectID, bucketID, objectKey, time.Now().Add(1*time.Hour))

	w := doRawRequest(t, env.router, http.MethodGet, "/files/"+uploadID, "", nil, nil)
	assertStatus(t, w, http.StatusNotFound)
}

func TestPresignRead_ValidPrivateBucket(t *testing.T) {
	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "private")
	uploadID := insertVerifiedUpload(t, env.db, projectID, bucketID, "images/test/presign.jpg", 1024, "private")

	w := doRequest(t, env.router, http.MethodPost, "/files/"+uploadID+"/read-presign", nil)
	assertStatus(t, w, http.StatusOK)

	body := decodeBody(t, w)
	if body["read_url"] == nil {
		t.Error("expected read_url in response")
	}
	if body["expires_at"] == nil {
		t.Error("expected expires_at in response")
	}
}

func TestPresignRead_PublicBucketRejected(t *testing.T) {
	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "public")
	uploadID := insertVerifiedUpload(t, env.db, projectID, bucketID, "images/test/pub.jpg", 1024, "public")

	w := doRequest(t, env.router, http.MethodPost, "/files/"+uploadID+"/read-presign", nil)
	assertStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "invalid_request")
}

func TestPresignRead_NotFound(t *testing.T) {
	env := newFullTestEnv(t)
	w := doRequest(t, env.router, http.MethodPost, "/files/nonexistent/read-presign", nil)
	assertStatus(t, w, http.StatusNotFound)
	assertErrorCode(t, w, "not_found")
}

func TestDeleteFile_Valid(t *testing.T) {
	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "public")
	objectKey := "images/test/del.jpg"
	env.seaweed.seed(objectKey, []byte("content"))
	uploadID := insertVerifiedUpload(t, env.db, projectID, bucketID, objectKey, 512000, "public")

	w := doRequest(t, env.router, http.MethodDelete, "/files/"+uploadID, nil)
	assertStatus(t, w, http.StatusOK)

	body := decodeBody(t, w)
	if body["deleted"] != true {
		t.Errorf("expected deleted true, got %v", body["deleted"])
	}
	if env.seaweed.deleteCallCount() == 0 {
		t.Error("expected DeleteObject to be called")
	}
}

func TestDeleteFile_QuotaDecrement(t *testing.T) {
	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "public")
	objectKey := "images/test/quota.jpg"
	sizeBytes := int64(512000)
	env.seaweed.seed(objectKey, make([]byte, sizeBytes))
	uploadID := insertVerifiedUpload(t, env.db, projectID, bucketID, objectKey, sizeBytes, "public")

	var usedBefore int64
	env.db.QueryRow("SELECT used_bytes FROM projects WHERE id = ?", projectID).Scan(&usedBefore)

	w := doRequest(t, env.router, http.MethodDelete, "/files/"+uploadID, nil)
	assertStatus(t, w, http.StatusOK)

	var usedAfter int64
	env.db.QueryRow("SELECT used_bytes FROM projects WHERE id = ?", projectID).Scan(&usedAfter)

	if usedBefore-usedAfter != sizeBytes {
		t.Errorf("expected used_bytes to decrease by %d, before=%d after=%d", sizeBytes, usedBefore, usedAfter)
	}
}

func TestDeleteFile_AlreadyDeleted(t *testing.T) {
	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "public")
	objectKey := "images/test/double-del.jpg"
	env.seaweed.seed(objectKey, []byte("content"))
	uploadID := insertVerifiedUpload(t, env.db, projectID, bucketID, objectKey, 1024, "public")

	doRequest(t, env.router, http.MethodDelete, "/files/"+uploadID, nil)

	w := doRequest(t, env.router, http.MethodDelete, "/files/"+uploadID, nil)
	assertStatus(t, w, http.StatusNotFound)
	assertErrorCode(t, w, "not_found")
}

func TestDeleteFile_PendingUpload(t *testing.T) {
	env := newFullTestEnv(t)
	projectID, bucketID := insertTestProject(t, env.db, "public")
	objectKey := "images/test/verified.jpg"
	env.seaweed.seed(objectKey, []byte("data"))
	uploadID := insertPendingUpload(t, env.db, projectID, bucketID, objectKey, time.Now().Add(1*time.Hour))

	w := doRequest(t, env.router, http.MethodDelete, "/files/"+uploadID, nil)
	assertStatus(t, w, http.StatusNotFound)
	assertErrorCode(t, w, "not_found")
}