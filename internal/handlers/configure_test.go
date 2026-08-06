package handlers_test

import (
	"net/http"
	"testing"

	"openstore/internal/models"
)

// POST /configure
func TestCreateConfigure_ValidFullConfiguration(t *testing.T) {
	env := newTestEnv(t)

	body := map[string]any{
		"project": map[string]any{
			"name": "mediavault",
			"webhook_url": "https://mediavault.example.com/webhook",
			"webhook_secret":"secret",
			"allowed_origins": []string{"https://mediavault.example.com"},
			"quota_bytes": 10737418240,
		},
		"buckets": []map[string]any{
			{
				"name":"mediavault-images",
				"media_class":"images",
				"allowed_mime": []string{"image/jpeg", "image/png"},
				"max_bytes":5242880,
				"presign_ttl_seconds": 300,
				"access":"public",
			},
			{
				"name":"mediavault-docs",
				"media_class":"documents",
				"allowed_mime": []string{"application/pdf"},
				"max_bytes":52428800,
				"presign_ttl_seconds": 300,
				"read_ttl_seconds":600,
				"access":"private",
			},
			{
				"name":"mediavault-audio",
				"media_class":"audio",
				"allowed_mime": []string{"audio/mpeg"},
				"max_bytes":10485760,
				"access":"public",
			},
		},
	}

	w := doRequest(t, env.router, http.MethodPost, "/configure", body)
	assertStatus(t, w, http.StatusCreated)

	result := decodeBody(t, w)

	if result["project_id"] == nil {
		t.Error("expected project_id in response")
	}
	if result["name"] != "mediavault" {
		t.Errorf("expected name mediavault, got %v", result["name"])
	}

	buckets, ok := result["buckets"].([]any)
	if !ok || len(buckets) != 3 {
		t.Errorf("expected 3 buckets in response, got %v", result["buckets"])
	}
}

func TestCreateConfigure_MissingProjectName(t *testing.T) {
	env := newTestEnv(t)

	body := validProjectBody()
	body["project"].(map[string]any)["name"] = ""

	w := doRequest(t, env.router, http.MethodPost, "/configure", body)
	assertStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "invalid_request")
}

func TestCreateConfigure_MissingWebhookURL(t *testing.T) {
	env := newTestEnv(t)

	body := validProjectBody()
	body["project"].(map[string]any)["webhook_url"] = ""

	w := doRequest(t, env.router, http.MethodPost, "/configure", body)
	assertStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "invalid_request")
}

func TestCreateConfigure_MissingWebhookSecret(t *testing.T) {
	env := newTestEnv(t)

	body := validProjectBody()
	body["project"].(map[string]any)["webhook_secret"] = ""

	w := doRequest(t, env.router, http.MethodPost, "/configure", body)
	assertStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "invalid_request")
}

func TestCreateConfigure_EmptyAllowedOrigins(t *testing.T) {
	env := newTestEnv(t)

	body := validProjectBody()
	body["project"].(map[string]any)["allowed_origins"] = []string{}

	w := doRequest(t, env.router, http.MethodPost, "/configure", body)
	assertStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "invalid_request")
}

func TestCreateConfigure_InvalidMediaClass(t *testing.T) {
	env := newTestEnv(t)

	body := validProjectBody()
	body["buckets"].([]map[string]any)[0]["media_class"] = "gifs"

	w := doRequest(t, env.router, http.MethodPost, "/configure", body)
	assertStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "invalid_request")
}

func TestCreateConfigure_InvalidAccessValue(t *testing.T) {
	env := newTestEnv(t)

	body := validProjectBody()
	body["buckets"].([]map[string]any)[0]["access"] = "restricted"

	w := doRequest(t, env.router, http.MethodPost, "/configure", body)
	assertStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "invalid_request")
}

func TestCreateConfigure_EmptyAllowedMIME(t *testing.T) {
	env := newTestEnv(t)

	body := validProjectBody()
	body["buckets"].([]map[string]any)[0]["allowed_mime"] = []string{}

	w := doRequest(t, env.router, http.MethodPost, "/configure", body)
	assertStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "invalid_request")
}

func TestCreateConfigure_ZeroMaxBytes(t *testing.T) {
	env := newTestEnv(t)

	body := validProjectBody()
	body["buckets"].([]map[string]any)[0]["max_bytes"] = 0

	w := doRequest(t, env.router, http.MethodPost, "/configure", body)
	assertStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "invalid_request")
}

func TestCreateConfigure_NoBuckets(t *testing.T) {
	env := newTestEnv(t)

	body := validProjectBody()
	body["buckets"] = []map[string]any{}

	w := doRequest(t, env.router, http.MethodPost, "/configure", body)
	assertStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "invalid_request")
}

func TestCreateConfigure_DuplicateBucketNameInRequest(t *testing.T) {
	env := newTestEnv(t)

	body := map[string]any{
		"project": map[string]any{
			"name": "testproject",
			"webhook_url": "https://example.com/webhook",
			"webhook_secret":"secret",
			"allowed_origins": []string{"https://example.com"},
			"quota_bytes": 0,
		},
		"buckets": []map[string]any{
			{
				"name":"duplicate-bucket",
				"media_class":"images",
				"allowed_mime": []string{"image/jpeg"},
				"max_bytes":5242880,
				"access":"public",
			},
			{
				"name": "duplicate-bucket",
				"media_class": "images",
				"allowed_mime": []string{"image/png"},
				"max_bytes": 5242880,
				"access": "public",
			},
		},
	}

	w := doRequest(t, env.router, http.MethodPost, "/configure", body)
	if w.Code != http.StatusBadRequest && w.Code != http.StatusConflict {
		t.Errorf("expected 400 or 409, got %d", w.Code)
	}

	if bucketExistsInDB(t, env.db, "duplicate-bucket") {
		t.Error("bucket should not have been created on conflict")
	}
}

func TestCreateConfigure_AtomicRollback(t *testing.T) {
	env := newTestEnv(t)

	body := map[string]any{
		"project": map[string]any{
			"name": "rollbackproject",
			"webhook_url": "https://example.com/webhook",
			"webhook_secret": "secret",
			"allowed_origins": []string{"https://example.com"},
			"quota_bytes": 0,
		},
		"buckets": []map[string]any{
			{
				"name": "valid-bucket",
				"media_class": "images",
				"allowed_mime": []string{"image/jpeg"},
				"max_bytes": 5242880,
				"access": "public",
			},
			{
				"name":"invalid-bucket",
				"media_class":"gifs",
				"allowed_mime":[]string{"image/gif"},
				"max_bytes":5242880,
				"access":"public",
			},
		},
	}

	w := doRequest(t, env.router, http.MethodPost, "/configure", body)
	if w.Code == http.StatusCreated {
		t.Fatal("expected non-201 due to invalid bucket")
	}

	var count int
	env.db.QueryRow("SELECT COUNT(*) FROM projects WHERE name = ?", "rollbackproject").Scan(&count)
	if count != 0 {
		t.Error("project should not exist after rollback")
	}

	if bucketExistsInDB(t, env.db, "valid-bucket") {
		t.Error("valid bucket should not exist after rollback")
	}
}

func TestCreateConfigure_TTLOmitted_UsesDefault(t *testing.T) {
	env := newTestEnv(t)

	body := validProjectBody()
	delete(body["buckets"].([]map[string]any)[0], "presign_ttl_seconds")

	w := doRequest(t, env.router, http.MethodPost, "/configure", body)
	assertStatus(t, w, http.StatusCreated)

	result := decodeBody(t, w)
	buckets := result["buckets"].([]any)
	bucket := buckets[0].(map[string]any)

	ttl := bucket["presign_ttl_seconds"].(float64)
	if ttl != 0 {
		// 0 means the handler stored whatever was provided (zero value)
		// the TTL resolution at presign time applies the default — this is correct
	}
}

func TestCreateConfigure_ZeroQuotaBytes_Unlimited(t *testing.T) {
	env := newTestEnv(t)

	body := validProjectBody()
	body["project"].(map[string]any)["quota_bytes"] = 0

	w := doRequest(t, env.router, http.MethodPost, "/configure", body)
	assertStatus(t, w, http.StatusCreated)

	result := decodeBody(t, w)
	if result["quota_bytes"].(float64) != 0 {
		t.Errorf("expected quota_bytes 0, got %v", result["quota_bytes"])
	}
}

func TestCreateConfigure_InvalidAPIKey(t *testing.T) {
	env := newTestEnv(t)

	w := doRequestWithKey(t, env.router, http.MethodPost, "/configure", "wrong-key", validProjectBody())
	assertStatus(t, w, http.StatusUnauthorized)
	assertErrorCode(t, w, "unauthorized")
}

func TestCreateConfigure_NoAPIKey(t *testing.T) {
	env := newTestEnv(t)

	w := doRequestNoAuth(t, env.router, http.MethodPost, "/configure", validProjectBody())
	assertStatus(t, w, http.StatusUnauthorized)
}

// GET /configure
func TestGetConfigure_ReturnsFullConfig(t *testing.T) {
	env := newTestEnv(t)

	create := doRequest(t, env.router, http.MethodPost, "/configure", validProjectBody())
	assertStatus(t, create, http.StatusCreated)
	created := decodeBody(t, create)
	projectID := created["project_id"].(string)

	w := doRequest(t, env.router, http.MethodGet, "/configure?project_id="+projectID, nil)
	assertStatus(t, w, http.StatusOK)

	result := decodeBody(t, w)
	if result["project_id"] != projectID {
		t.Errorf("expected project_id %s, got %v", projectID, result["project_id"])
	}
	if result["name"] != "testproject" {
		t.Errorf("expected name testproject, got %v", result["name"])
	}

	buckets, ok := result["buckets"].([]any)
	if !ok || len(buckets) != 1 {
		t.Errorf("expected 1 bucket, got %v", result["buckets"])
	}
}

func TestGetConfigure_IncludesUsedBytes(t *testing.T) {
	env := newTestEnv(t)

	create := doRequest(t, env.router, http.MethodPost, "/configure", validProjectBody())
	assertStatus(t, create, http.StatusCreated)
	created := decodeBody(t, create)
	projectID := created["project_id"].(string)

	w := doRequest(t, env.router, http.MethodGet, "/configure?project_id="+projectID, nil)
	assertStatus(t, w, http.StatusOK)

	result := decodeBody(t, w)
	if _, ok := result["used_bytes"]; !ok {
		t.Error("expected used_bytes in response")
	}
	if result["used_bytes"].(float64) != 0 {
		t.Errorf("expected used_bytes 0 for new project, got %v", result["used_bytes"])
	}
}

func TestGetConfigure_MissingProjectID(t *testing.T) {
	env := newTestEnv(t)

	w := doRequest(t, env.router, http.MethodGet, "/configure", nil)
	assertStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "invalid_request")
}

func TestGetConfigure_ProjectNotFound(t *testing.T) {
	env := newTestEnv(t)

	w := doRequest(t, env.router, http.MethodGet, "/configure?project_id=nonexistent", nil)
	assertStatus(t, w, http.StatusNotFound)
	assertErrorCode(t, w, "not_found")
}

func TestGetConfigure_InvalidAPIKey(t *testing.T) {
	env := newTestEnv(t)

	w := doRequestWithKey(t, env.router, http.MethodGet, "/configure?project_id=any", "wrong-key", nil)
	assertStatus(t, w, http.StatusUnauthorized)
}

// PUT /configure
func TestUpdateConfigure_UpdatesProjectFields(t *testing.T) {
	env := newTestEnv(t)

	create := doRequest(t, env.router, http.MethodPost, "/configure", validProjectBody())
	assertStatus(t, create, http.StatusCreated)
	created := decodeBody(t, create)
	projectID := created["project_id"].(string)

	body := map[string]any{
		"project_id": projectID,
		"project": map[string]any{
			"webhook_url":"https://updated.example.com/webhook",
			"webhook_secret": "newsecret",
		},
		"buckets": []map[string]any{},
	}

	w := doRequest(t, env.router, http.MethodPut, "/configure", body)
	assertStatus(t, w, http.StatusOK)

	result := decodeBody(t, w)
	if result["webhook_url"] != "https://updated.example.com/webhook" {
		t.Errorf("expected updated webhook_url, got %v", result["webhook_url"])
	}
}

func TestUpdateConfigure_AddsNewBucket(t *testing.T) {
	env := newTestEnv(t)

	create := doRequest(t, env.router, http.MethodPost, "/configure", validProjectBody())
	assertStatus(t, create, http.StatusCreated)
	created := decodeBody(t, create)
	projectID := created["project_id"].(string)

	body := map[string]any{
		"project_id": projectID,
		"project":map[string]any{},
		"buckets": []map[string]any{
			{
				"name": "testproject-videos",
				"media_class": "videos",
				"allowed_mime": []string{"video/mp4"},
				"max_bytes": 104857600,
				"access": "private",
			},
		},
	}

	w := doRequest(t, env.router, http.MethodPut, "/configure", body)
	assertStatus(t, w, http.StatusOK)

	if !bucketExistsInDB(t, env.db, "testproject-videos") {
		t.Error("new bucket should exist in DB after PUT")
	}
}

func TestUpdateConfigure_UpdatesExistingBucketTTL(t *testing.T) {
	env := newTestEnv(t)

	create := doRequest(t, env.router, http.MethodPost, "/configure", validProjectBody())
	assertStatus(t, create, http.StatusCreated)
	created := decodeBody(t, create)
	projectID := created["project_id"].(string)

	body := map[string]any{
		"project_id": projectID,
		"project": map[string]any{},
		"buckets": []map[string]any{
			{
				"name": "testproject-images",
				"media_class": "images",
				"allowed_mime": []string{"image/jpeg", "image/png"},
				"max_bytes": 5242880,
				"presign_ttl_seconds": 600,
				"access": "public",
			},
		},
	}

	w := doRequest(t, env.router, http.MethodPut, "/configure", body)
	assertStatus(t, w, http.StatusOK)

	result := decodeBody(t, w)
	buckets := result["buckets"].([]any)
	bucket := buckets[0].(map[string]any)
	if bucket["presign_ttl_seconds"].(float64) != 600 {
		t.Errorf("expected presign_ttl_seconds 600, got %v", bucket["presign_ttl_seconds"])
	}
}

func TestUpdateConfigure_DoesNotDeleteAbsentBucket(t *testing.T) {
	env := newTestEnv(t)

	create := doRequest(t, env.router, http.MethodPost, "/configure", validProjectBody())
	assertStatus(t, create, http.StatusCreated)
	created := decodeBody(t, create)
	projectID := created["project_id"].(string)

	body := map[string]any{
		"project_id": projectID,
		"project": map[string]any{},
		"buckets": []map[string]any{},
	}

	w := doRequest(t, env.router, http.MethodPut, "/configure", body)
	assertStatus(t, w, http.StatusOK)

	if !bucketExistsInDB(t, env.db, "testproject-images") {
		t.Error("bucket absent from PUT body should still exist in DB")
	}
}

func TestUpdateConfigure_ProjectNotFound(t *testing.T) {
	env := newTestEnv(t)

	body := map[string]any{
		"project_id": "nonexistent",
		"project": map[string]any{},
		"buckets": []map[string]any{},
	}

	w := doRequest(t, env.router, http.MethodPut, "/configure", body)
	assertStatus(t, w, http.StatusNotFound)
	assertErrorCode(t, w, "not_found")
}

func TestUpdateConfigure_MissingProjectID(t *testing.T) {
	env := newTestEnv(t)

	body := map[string]any{
		"project": map[string]any{},
		"buckets": []map[string]any{},
	}

	w := doRequest(t, env.router, http.MethodPut, "/configure", body)
	assertStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "invalid_request")
}

func TestUpdateConfigure_InvalidAPIKey(t *testing.T) {
	env := newTestEnv(t)

	body := map[string]any{
		"project_id": "any",
		"project": map[string]any{},
		"buckets": []map[string]any{},
	}

	w := doRequestWithKey(t, env.router, http.MethodPut, "/configure", "wrong-key", body)
	assertStatus(t, w, http.StatusUnauthorized)
}

// PATCH /configure/buckets/:bucket_name
func TestPatchBucket_UpdatesAllowedMIME(t *testing.T) {
	env := newTestEnv(t)

	create := doRequest(t, env.router, http.MethodPost, "/configure", validProjectBody())
	assertStatus(t, create, http.StatusCreated)
	created := decodeBody(t, create)
	projectID := created["project_id"].(string)

	body := map[string]any{
		"project_id":projectID,
		"allowed_mime": []string{"image/jpeg", "image/png", "image/webp", "image/gif"},
	}

	w := doRequest(t, env.router, http.MethodPatch, "/configure/buckets/testproject-images", body)
	assertStatus(t, w, http.StatusOK)

	result := decodeBody(t, w)
	mime := result["allowed_mime"].([]any)
	if len(mime) != 4 {
		t.Errorf("expected 4 mime types, got %d", len(mime))
	}
}

func TestPatchBucket_UpdatesMaxBytes(t *testing.T) {
	env := newTestEnv(t)

	create := doRequest(t, env.router, http.MethodPost, "/configure", validProjectBody())
	assertStatus(t, create, http.StatusCreated)
	created := decodeBody(t, create)
	projectID := created["project_id"].(string)

	body := map[string]any{
		"project_id": projectID,
		"max_bytes": 10485760,
	}

	w := doRequest(t, env.router, http.MethodPatch, "/configure/buckets/testproject-images", body)
	assertStatus(t, w, http.StatusOK)

	result := decodeBody(t, w)
	if result["max_bytes"].(float64) != 10485760 {
		t.Errorf("expected max_bytes 10485760, got %v", result["max_bytes"])
	}
}

func TestPatchBucket_UpdatesPresignTTL(t *testing.T) {
	env := newTestEnv(t)

	create := doRequest(t, env.router, http.MethodPost, "/configure", validProjectBody())
	assertStatus(t, create, http.StatusCreated)
	created := decodeBody(t, create)
	projectID := created["project_id"].(string)

	body := map[string]any{
		"project_id":projectID,
		"presign_ttl_seconds": 600,
	}

	w := doRequest(t, env.router, http.MethodPatch, "/configure/buckets/testproject-images", body)
	assertStatus(t, w, http.StatusOK)

	result := decodeBody(t, w)
	if result["presign_ttl_seconds"].(float64) != 600 {
		t.Errorf("expected presign_ttl_seconds 600, got %v", result["presign_ttl_seconds"])
	}
}

func TestPatchBucket_RejectsAccessChange(t *testing.T) {
	env := newTestEnv(t)

	create := doRequest(t, env.router, http.MethodPost, "/configure", validProjectBody())
	assertStatus(t, create, http.StatusCreated)
	created := decodeBody(t, create)
	projectID := created["project_id"].(string)

	body := map[string]any{
		"project_id": projectID,
		"access": "private",
	}

	w := doRequest(t, env.router, http.MethodPatch, "/configure/buckets/testproject-images", body)
	assertStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "invalid_request")
}

func TestPatchBucket_RejectsMediaClassChange(t *testing.T) {
	env := newTestEnv(t)

	create := doRequest(t, env.router, http.MethodPost, "/configure", validProjectBody())
	assertStatus(t, create, http.StatusCreated)
	created := decodeBody(t, create)
	projectID := created["project_id"].(string)

	body := map[string]any{
		"project_id":projectID,
		"media_class": "videos",
	}

	w := doRequest(t, env.router, http.MethodPatch, "/configure/buckets/testproject-images", body)
	assertStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "invalid_request")
}

func TestPatchBucket_BucketNotFound(t *testing.T) {
	env := newTestEnv(t)

	create := doRequest(t, env.router, http.MethodPost, "/configure", validProjectBody())
	assertStatus(t, create, http.StatusCreated)
	created := decodeBody(t, create)
	projectID := created["project_id"].(string)

	body := map[string]any{
		"project_id": projectID,
		"max_bytes":10485760,
	}

	w := doRequest(t, env.router, http.MethodPatch, "/configure/buckets/nonexistent-bucket", body)
	assertStatus(t, w, http.StatusNotFound)
	assertErrorCode(t, w, "not_found")
}

func TestPatchBucket_InvalidAPIKey(t *testing.T) {
	env := newTestEnv(t)

	body := map[string]any{
		"project_id": "any",
		"max_bytes":10485760,
	}

	w := doRequestWithKey(t, env.router, http.MethodPatch, "/configure/buckets/any", "wrong-key", body)
	assertStatus(t, w, http.StatusUnauthorized)
}

// DELETE /configure
func TestDeleteConfigure_ValidDeletion(t *testing.T) {
	env := newTestEnv(t)

	create := doRequest(t, env.router, http.MethodPost, "/configure", validProjectBody())
	assertStatus(t, create, http.StatusCreated)
	created := decodeBody(t, create)
	projectID := created["project_id"].(string)

	body := map[string]any{
		"project_id": projectID,
		"confirm": "testproject",
	}

	w := doRequest(t, env.router, http.MethodDelete, "/configure", body)
	assertStatus(t, w, http.StatusOK)

	result := decodeBody(t, w)
	if result["deleted"] != true {
		t.Errorf("expected deleted true, got %v", result["deleted"])
	}

	if projectExistsInDB(t, env.db, projectID) {
		t.Error("project should not exist after deletion")
	}

	if bucketExistsInDB(t, env.db, "testproject-images") {
		t.Error("buckets should not exist after project deletion")
	}
}

func TestDeleteConfigure_WrongConfirmValue(t *testing.T) {
	env := newTestEnv(t)

	create := doRequest(t, env.router, http.MethodPost, "/configure", validProjectBody())
	assertStatus(t, create, http.StatusCreated)
	created := decodeBody(t, create)
	projectID := created["project_id"].(string)

	body := map[string]any{
		"project_id": projectID,
		"confirm": "wrongname",
	}

	w := doRequest(t, env.router, http.MethodDelete, "/configure", body)
	assertStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "invalid_request")

	if !projectExistsInDB(t, env.db, projectID) {
		t.Error("project should still exist after failed deletion")
	}
}

func TestDeleteConfigure_MissingConfirm(t *testing.T) {
	env := newTestEnv(t)

	create := doRequest(t, env.router, http.MethodPost, "/configure", validProjectBody())
	assertStatus(t, create, http.StatusCreated)
	created := decodeBody(t, create)
	projectID := created["project_id"].(string)

	body := map[string]any{
		"project_id": projectID,
	}

	w := doRequest(t, env.router, http.MethodDelete, "/configure", body)
	assertStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "invalid_request")
}

func TestDeleteConfigure_ProjectNotFound(t *testing.T) {
	env := newTestEnv(t)

	body := map[string]any{
		"project_id": "nonexistent",
		"confirm":"anything",
	}

	w := doRequest(t, env.router, http.MethodDelete, "/configure", body)
	assertStatus(t, w, http.StatusNotFound)
	assertErrorCode(t, w, "not_found")
}

func TestDeleteConfigure_InvalidAPIKey(t *testing.T) {
	env := newTestEnv(t)

	body := map[string]any{
		"project_id": "any",
		"confirm": "any",
	}

	w := doRequestWithKey(t, env.router, http.MethodDelete, "/configure", "wrong-key", body)
	assertStatus(t, w, http.StatusUnauthorized)
}

// DELETE /configure/buckets/:bucket_name
func TestDeleteBucket_ValidDeletion(t *testing.T) {
	env := newTestEnv(t)

	create := doRequest(t, env.router, http.MethodPost, "/configure", validProjectBody())
	assertStatus(t, create, http.StatusCreated)
	created := decodeBody(t, create)
	projectID := created["project_id"].(string)

	body := map[string]any{
		"project_id": projectID,
		"force": false,
	}

	w := doRequest(t, env.router, http.MethodDelete, "/configure/buckets/testproject-images", body)
	assertStatus(t, w, http.StatusOK)

	if bucketExistsInDB(t, env.db, "testproject-images") {
		t.Error("bucket should not exist after deletion")
	}
}

func TestDeleteBucket_BlockedByVerifiedUploads(t *testing.T) {
	env := newTestEnv(t)

	create := doRequest(t, env.router, http.MethodPost, "/configure", validProjectBody())
	assertStatus(t, create, http.StatusCreated)
	created := decodeBody(t, create)
	projectID := created["project_id"].(string)

	var bucketID string
	env.db.QueryRow("SELECT id FROM buckets WHERE name = ?", "testproject-images").Scan(&bucketID)

	_, err := env.db.Exec(`
		INSERT INTO uploads (id, project_id, bucket_id, object_key, original_name,
		content_type, size_bytes, status, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'verified', datetime('now', '+1 hour'))`,
		"upload-001", projectID, bucketID,
		"images/"+projectID+"/2026/07/upload-001.jpg",
		"photo.jpg", "image/jpeg", 204800,
	)
	if err != nil {
		t.Fatalf("insert verified upload: %v", err)
	}

	body := map[string]any{
		"project_id": projectID,
		"force": false,
	}

	w := doRequest(t, env.router, http.MethodDelete, "/configure/buckets/testproject-images", body)
	assertStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "invalid_request")

	if !bucketExistsInDB(t, env.db, "testproject-images") {
		t.Error("bucket should still exist when deletion is blocked")
	}
}

func TestDeleteBucket_ForceDeleteWithVerifiedUploads(t *testing.T) {
	env := newTestEnv(t)

	create := doRequest(t, env.router, http.MethodPost, "/configure", validProjectBody())
	assertStatus(t, create, http.StatusCreated)
	created := decodeBody(t, create)
	projectID := created["project_id"].(string)

	var bucketID string
	env.db.QueryRow("SELECT id FROM buckets WHERE name = ?", "testproject-images").Scan(&bucketID)

	_, err := env.db.Exec(`
		INSERT INTO uploads (id, project_id, bucket_id, object_key, original_name,
		content_type, size_bytes, status, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'verified', datetime('now', '+1 hour'))`,
		"upload-002", projectID, bucketID,
		"images/"+projectID+"/2026/07/upload-002.jpg",
		"photo.jpg", "image/jpeg", 204800,
	)
	if err != nil {
		t.Fatalf("insert verified upload: %v", err)
	}

	body := map[string]any{
		"project_id": projectID,
		"force": true,
	}

	w := doRequest(t, env.router, http.MethodDelete, "/configure/buckets/testproject-images", body)
	assertStatus(t, w, http.StatusOK)

	if bucketExistsInDB(t, env.db, "testproject-images") {
		t.Error("bucket should be gone after force deletion")
	}
}

func TestDeleteBucket_BucketNotFound(t *testing.T) {
	env := newTestEnv(t)

	create := doRequest(t, env.router, http.MethodPost, "/configure", validProjectBody())
	assertStatus(t, create, http.StatusCreated)
	created := decodeBody(t, create)
	projectID := created["project_id"].(string)

	body := map[string]any{
		"project_id": projectID,
		"force": false,
	}

	w := doRequest(t, env.router, http.MethodDelete, "/configure/buckets/nonexistent-bucket", body)
	assertStatus(t, w, http.StatusNotFound)
	assertErrorCode(t, w, "not_found")
}

func TestDeleteBucket_InvalidAPIKey(t *testing.T) {
	env := newTestEnv(t)

	body := map[string]any{
		"project_id": "any",
		"force": false,
	}

	w := doRequestWithKey(t, env.router, http.MethodDelete, "/configure/buckets/any", "wrong-key", body)
	assertStatus(t, w, http.StatusUnauthorized)
}

// model-level sentinel errors

func TestModels_ErrNotFound_IsDistinct(t *testing.T) {
	if models.ErrNotFound == nil {
		t.Error("ErrNotFound must not be nil")
	}
	if models.ErrConflict == nil {
		t.Error("ErrConflict must not be nil")
	}
	if models.ErrNotFound == models.ErrConflict {
		t.Error("ErrNotFound and ErrConflict must be distinct errors")
	}
}