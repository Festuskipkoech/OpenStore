package handlers
 
import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
 
	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"
 
	"openstore/internal/models"
)

type ConfigureHandler struct {
	db *sql.DB
}
 
func NewConfigureHandler(db *sql.DB) *ConfigureHandler {
	return &ConfigureHandler{db: db}
}

// POST /configure
// Creates a project and its buckets atomically. Any validation failure rolls back entirely.
type configureBucketInput struct {
	Name string  `json:"name"`
	MediaClass string `json:"media_class"`
	AllowedMIME []string `json:"allowed_mime"`
	MaxBytes int64 `json:"max_bytes"`
	PresignTTLSeconds int `json:"presign_ttl_seconds"`
	ReadTTLSeconds int `json:"read_ttl_seconds"`
	Access string `json:"access"`
}
 
type configureProjectInput struct {
	Name string `json:"name"`
	WebhookURL string `json:"webhook_url"`
	WebhookSecret string `json:"webhook_secret"`
	AllowedOrigins []string `json:"allowed_origins"`
	QuotaBytes int64 `json:"quota_bytes"`
}
 
type createConfigureRequest struct {
	Project configureProjectInput  `json:"project"`
	Buckets []configureBucketInput `json:"buckets"`
}
 
type configureResponse struct {
	ProjectID string `json:"project_id"`
	Name string `json:"name"`
	WebhookURL string  `json:"webhook_url"`
	AllowedOrigins []string `json:"allowed_origins"`
	QuotaBytes int64 `json:"quota_bytes"`
	UsedBytes int64 `json:"used_bytes"`
	Buckets []*models.Bucket `json:"buckets"`
	CreatedAt time.Time `json:"created_at"`
}

type putConfigureRequest struct {
	ProjectID string `json:"project_id"`
	Project configureProjectInput `json:"project"`
	Buckets []configureBucketInput `json:"buckets"`
}

type patchBucketRequest struct {
	ProjectID string `json:"project_id"`
	AllowedMIME []string `json:"allowed_mime"`
	MaxBytes *int64 `json:"max_bytes"`
	PresignTTLSeconds *int `json:"presign_ttl_seconds"`
	ReadTTLSeconds *int `json:"read_ttl_seconds"`
	Access *string `json:"access"`
	MediaClass *string `json:"media_class"`
}

type deleteConfigureRequest struct {
	ProjectID string `json:"project_id"`
	Confirm string `json:"confirm"`
}

type deleteBucketRequest struct {
	ProjectID string `json:"project_id"`
	Force bool `json:"force"`
}


func (h *ConfigureHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createConfigureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "invalid_request")
		return
	}
 
	if err := validateProjectInput(req.Project); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request")
		return
	}
 
	if len(req.Buckets) == 0 {
		writeError(w, http.StatusBadRequest, "at least one bucket is required", "invalid_request")
		return
	}
 
	for i, b := range req.Buckets {
		if err := validateBucketInput(b); err != nil {
			writeError(w, http.StatusBadRequest, "bucket["+itoa(i)+"]: "+err.Error(), "invalid_request")
			return
		}
	}
 
	tx, err := h.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin transaction", "internal_error")
		return
	}
	defer tx.Rollback()
 
	project, err := models.CreateProjectTx(tx, models.CreateProjectParams{
		ID: ulid.Make().String(),
		Name: req.Project.Name,
		APIKeyHash: "",
		WebhookURL: req.Project.WebhookURL,
		WebhookSecret: req.Project.WebhookSecret,
		AllowedOrigins: req.Project.AllowedOrigins,
		QuotaBytes: req.Project.QuotaBytes,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create project", "internal_error")
		return
	}
 
	buckets := make([]*models.Bucket, 0, len(req.Buckets))
	for _, b := range req.Buckets {
		bucket, err := models.CreateBucketTx(tx, models.CreateBucketParams{
			ID: ulid.Make().String(),
			ProjectID: project.ID,
			Name: b.Name,
			MediaClass: b.MediaClass,
			AllowedMIME: b.AllowedMIME,
			MaxBytes: b.MaxBytes,
			PresignTTLSeconds: b.PresignTTLSeconds,
			ReadTTLSeconds: b.ReadTTLSeconds,
			Access: b.Access,
		})
		if err != nil {
			if errors.Is(err, models.ErrConflict) {
				writeError(w, http.StatusConflict, "bucket name already exists: "+b.Name, "conflict")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to create bucket: "+b.Name, "internal_error")
			return
		}
		buckets = append(buckets, bucket)
	}
 
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit transaction", "internal_error")
		return
	}
 
	writeJSON(w, http.StatusCreated, configureResponse{
		ProjectID: project.ID,
		Name: project.Name,
		WebhookURL: project.WebhookURL,
		AllowedOrigins: project.AllowedOrigins,
		QuotaBytes: project.QuotaBytes,
		UsedBytes: project.UsedBytes,
		Buckets: buckets,
		CreatedAt: project.CreatedAt,
	})
}

// GET /configure
// Returns the project with all buckets and current used_bytes.
// Requires project_id query param since there is no per-project auth yet. 
func (h *ConfigureHandler) Get(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "project_id query parameter is required", "invalid_request")
		return
	}
 
	project, err := models.GetProject(h.db, projectID)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeError(w, http.StatusNotFound, "project not found", "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get project", "internal_error")
		return
	}
 
	buckets, err := models.GetAllBucketsForProject(h.db, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get buckets", "internal_error")
		return
	}
 
	if buckets == nil {
		buckets = []*models.Bucket{}
	}
 
	writeJSON(w, http.StatusOK, configureResponse{
		ProjectID: project.ID,
		Name: project.Name,
		WebhookURL: project.WebhookURL,
		AllowedOrigins: project.AllowedOrigins,
		QuotaBytes: project.QuotaBytes,
		UsedBytes: project.UsedBytes,
		Buckets: buckets,
		CreatedAt: project.CreatedAt,
	})
}

// PUT /configure
// Reconciles project fields and buckets. Does not delete buckets absent from the request.
func (h *ConfigureHandler) Update(w http.ResponseWriter, r *http.Request) {
	var req putConfigureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "invalid_request")
		return
	}
 
	if req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, "project_id is required", "invalid_request")
		return
	}
 
	for i, b := range req.Buckets {
		if err := validateBucketInput(b); err != nil {
			writeError(w, http.StatusBadRequest, "bucket["+itoa(i)+"]: "+err.Error(), "invalid_request")
			return
		}
	}
 
	tx, err := h.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin transaction", "internal_error")
		return
	}
	defer tx.Rollback()
 
	webhookURL := nonemptyPtr(req.Project.WebhookURL)
	webhookSecret := nonemptyPtr(req.Project.WebhookSecret)
	name := nonemptyPtr(req.Project.Name)
	var quotaPtr *int64
	if req.Project.QuotaBytes != 0 {
		q := req.Project.QuotaBytes
		quotaPtr = &q
	}
 
	project, err := models.UpdateProjectTx(tx, req.ProjectID, models.UpdateProjectParams{
		Name: name,
		WebhookURL: webhookURL,
		WebhookSecret: webhookSecret,
		AllowedOrigins: req.Project.AllowedOrigins,
		QuotaBytes: quotaPtr,
	})
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeError(w, http.StatusNotFound, "project not found", "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update project", "internal_error")
		return
	}
 
	buckets := make([]*models.Bucket, 0)
	for _, b := range req.Buckets {
		existing, err := models.GetBucketByNameTx(tx, req.ProjectID, b.Name)
		if err != nil && !errors.Is(err, models.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "failed to look up bucket", "internal_error")
			return
		}
 
		if errors.Is(err, models.ErrNotFound) {
			bucket, err := models.CreateBucketTx(tx, models.CreateBucketParams{
				ID: ulid.Make().String(),
				ProjectID: project.ID,
				Name: b.Name,
				MediaClass: b.MediaClass,
				AllowedMIME: b.AllowedMIME,
				MaxBytes: b.MaxBytes,
				PresignTTLSeconds: b.PresignTTLSeconds,
				ReadTTLSeconds: b.ReadTTLSeconds,
				Access: b.Access,
			})
			if err != nil {
				if errors.Is(err, models.ErrConflict) {
					writeError(w, http.StatusConflict, "bucket name already exists: "+b.Name, "conflict")
					return
				}
				writeError(w, http.StatusInternalServerError, "failed to create bucket", "internal_error")
				return
			}
			buckets = append(buckets, bucket)
		} else {
			ttl := b.PresignTTLSeconds
			readTTL := b.ReadTTLSeconds
			updated, err := models.UpdateBucketTx(tx, existing.ID, models.UpdateBucketParams{
				AllowedMIME: b.AllowedMIME,
				MaxBytes: &b.MaxBytes,
				PresignTTLSeconds: &ttl,
				ReadTTLSeconds: &readTTL,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update bucket", "internal_error")
				return
			}
			buckets = append(buckets, updated)
		}
	}
 
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit transaction", "internal_error")
		return
	}
 
	writeJSON(w, http.StatusOK, configureResponse{
		ProjectID: project.ID,
		Name: project.Name,
		WebhookURL: project.WebhookURL,
		AllowedOrigins: project.AllowedOrigins,
		QuotaBytes: project.QuotaBytes,
		UsedBytes: project.UsedBytes,
		Buckets: buckets,
		CreatedAt: project.CreatedAt,
	})
}

// PATCH /configure/buckets/:bucket_name
// Partial bucket update. Blocks changes to access and media_class.
func (h *ConfigureHandler) PatchBucket(w http.ResponseWriter, r *http.Request) {
	bucketName := chi.URLParam(r, "bucketName")
 
	var req patchBucketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "invalid_request")
		return
	}
 
	if req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, "project_id is required", "invalid_request")
		return
	}
 
	if req.Access != nil {
		writeError(w, http.StatusBadRequest, "access cannot be changed after creation", "invalid_request")
		return
	}
	if req.MediaClass != nil {
		writeError(w, http.StatusBadRequest, "media_class cannot be changed after creation", "invalid_request")
		return
	}
 
	bucket, err := models.GetBucketByName(h.db, req.ProjectID, bucketName)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeError(w, http.StatusNotFound, "bucket not found", "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get bucket", "internal_error")
		return
	}
 
	updated, err := models.UpdateBucket(h.db, bucket.ID, models.UpdateBucketParams{
		AllowedMIME: req.AllowedMIME,
		MaxBytes: req.MaxBytes,
		PresignTTLSeconds: req.PresignTTLSeconds,
		ReadTTLSeconds: req.ReadTTLSeconds,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update bucket", "internal_error")
		return
	}
 
	writeJSON(w, http.StatusOK, updated)
}

// DELETE /configure
// Deletes the project and all associated data. Requires confirm field matching project name.
func (h *ConfigureHandler) Delete(w http.ResponseWriter, r *http.Request) {
	var req deleteConfigureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "invalid_request")
		return
	}
 
	if req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, "project_id is required", "invalid_request")
		return
	}
 
	project, err := models.GetProject(h.db, req.ProjectID)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeError(w, http.StatusNotFound, "project not found", "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get project", "internal_error")
		return
	}
 
	if req.Confirm != project.Name {
		writeError(w, http.StatusBadRequest, "confirm value does not match project name", "invalid_request")
		return
	}
 
	if err := models.DeleteProject(h.db, req.ProjectID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete project", "internal_error")
		return
	}
 
	writeJSON(w, http.StatusOK, map[string]any{
		"project_id": req.ProjectID,
		"deleted": true,
	})
}

// DELETE /configure/buckets/:bucket_name
// Deletes a bucket. Blocked if verified uploads exist unless force: true.
func (h *ConfigureHandler) DeleteBucket(w http.ResponseWriter, r *http.Request) {
	bucketName := chi.URLParam(r, "bucketName")
 
	var req deleteBucketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "invalid_request")
		return
	}
 
	if req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, "project_id is required", "invalid_request")
		return
	}
 
	bucket, err := models.GetBucketByName(h.db, req.ProjectID, bucketName)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeError(w, http.StatusNotFound, "bucket not found", "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get bucket", "internal_error")
		return
	}
 
	if !req.Force {
		hasUploads, err := models.BucketHasVerifiedUploads(h.db, bucket.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to check uploads", "internal_error")
			return
		}
		if hasUploads {
			writeError(w, http.StatusBadRequest, "bucket has verified uploads, use force: true to delete anyway", "invalid_request")
			return
		}
	}
 
	if err := models.DeleteBucket(h.db, bucket.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete bucket", "internal_error")
		return
	}
 
	writeJSON(w, http.StatusOK, map[string]any{
		"bucket_name": bucketName,
		"deleted": true,
	})
}

func validateProjectInput(p configureProjectInput) error {
	if p.Name == "" {
		return errors.New("project name is required")
	}
	if p.WebhookURL == "" {
		return errors.New("webhook_url is required")
	}
	if p.WebhookSecret == "" {
		return errors.New("webhook_secret is required")
	}
	if len(p.AllowedOrigins) == 0 {
		return errors.New("allowed_origins must not be empty")
	}
	return nil
}
 
func validateBucketInput(b configureBucketInput) error {
	if b.Name == "" {
		return errors.New("bucket name is required")
	}
	if !models.ValidMediaClasses[b.MediaClass] {
		return errors.New("media_class must be one of: images, videos, audio, documents")
	}
	if len(b.AllowedMIME) == 0 {
		return errors.New("allowed_mime must not be empty")
	}
	if b.MaxBytes <= 0 {
		return errors.New("max_bytes must be greater than zero")
	}
	access := b.Access
	if access == "" {
		access = "public"
	}
	if !models.ValidAccessValues[access] {
		return errors.New("access must be one of: public, private")
	}
	return nil
}
 
 
func nonemptyPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
 
func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}