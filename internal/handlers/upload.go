package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"openstore/internal/config"
	"openstore/internal/models"
	"openstore/internal/quota"
	"openstore/internal/security"
)

// SeaweedClient is the interface satisfied by both *seaweedfs.Client in
// production and the mock injected by tests.
type SeaweedClient interface {
	WriteObject(ctx context.Context, objectKey, contentType string, r io.Reader) error
	DeleteObject(ctx context.Context, objectKey string) error
	ReadObject(ctx context.Context, objectKey string, w io.Writer) error
	PublicURL(objectKey string) string
}

type UploadHandler struct {
	db *sql.DB
	seaweed SeaweedClient
	tokenizer *security.Tokenizer
	cfg *config.Config
}

func NewUploadHandler(db *sql.DB, seaweed SeaweedClient, tokenizer *security.Tokenizer, cfg *config.Config) *UploadHandler {
	return &UploadHandler{
		db: db,
		seaweed: seaweed,
		tokenizer: tokenizer,
		cfg: cfg,
	}
}

var NewUploadHandlerWithSeaweed = NewUploadHandler

type presignRequest struct {
	BucketName string `json:"bucket_name"`
	Filename string `json:"filename"`
	MIMEType string `json:"mime_type"`
	FileSize int64 `json:"file_size"`
}

type presignResponse struct {
	UploadID string `json:"upload_id"`
	UploadURL string `json:"upload_url"`
	Method string `json:"method"`
	ObjectKey string `json:"object_key"`
	Bucket string `json:"bucket"`
	ExpiresAt time.Time `json:"expires_at"`
	Headers map[string]string `json:"headers"`
}

// Presign handles POST /upload/presign.
// Validates the bucket, MIME type, file size, and quota headroom, then
// creates a pending upload record and returns a signed URL pointing to
// PUT /upload/{upload_id} on OpenStore itself.
func (h *UploadHandler) Presign(w http.ResponseWriter, r *http.Request) {
	var req presignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "invalid_request")
		return
	}

	if err := validatePresignRequest(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request")
		return
	}

	// Resolve the project ID from the API key. The auth middleware has already
	// verified the key is valid — we need the project that owns this bucket.
	bucket, err := models.GetBucketByNameForPresign(h.db, req.BucketName)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeError(w, http.StatusNotFound, "bucket not found", "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to look up bucket", "internal_error")
		return
	}

	if !security.IsAllowedMIME(req.MIMEType, bucket.AllowedMIME) {
		writeError(w, http.StatusUnprocessableEntity, "mime_type not allowed for this bucket", "mime_not_allowed")
		return
	}

	if req.FileSize > bucket.MaxBytes {
		writeError(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("file_size exceeds bucket maximum of %d bytes", bucket.MaxBytes),
			"size_exceeded",
		)
		return
	}

	if err := quota.CheckHeadroom(h.db, bucket.ProjectID, req.FileSize); err != nil {
		if errors.Is(err, quota.ErrQuotaExceeded) {
			writeError(w, http.StatusTooManyRequests, "file size would exceed project quota", "quota_exceeded")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to check quota", "internal_error")
		return
	}

	ttlSeconds := config.ResolveTTL(
		0,
		bucket.PresignTTLSeconds,
		h.cfg.PresignTTLDefault,
		h.cfg.PresignTTLMax,
	)

	uploadID := ulid.Make().String()
	objectKey := buildObjectKey(bucket.MediaClass, bucket.ProjectID, uploadID, req.Filename)
	expiresAt := time.Now().UTC().Add(time.Duration(ttlSeconds) * time.Second)

	token := h.tokenizer.Sign(security.TokenClaims{
		UploadID: uploadID,
		BucketName: req.BucketName,
		MIMEType: req.MIMEType,
		FileSize: req.FileSize,
		ExpiresAt: expiresAt,
	})

	_, err = models.CreateUpload(h.db, models.CreateUploadParams{
		ID: uploadID,
		ProjectID: bucket.ProjectID,
		BucketID: bucket.ID,
		ObjectKey: objectKey,
		OriginalName: req.Filename,
		ContentType: req.MIMEType,
		SizeBytes: req.FileSize,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create upload record", "internal_error")
		return
	}

	uploadURL := buildUploadURL(r, uploadID, token)

	writeJSON(w, http.StatusCreated, presignResponse{
		UploadID: uploadID,
		UploadURL: uploadURL,
		Method: http.MethodPut,
		ObjectKey: objectKey,
		Bucket: req.BucketName,
		ExpiresAt: expiresAt,
		Headers: map[string]string{
			"Content-Type": req.MIMEType,
			"Content-Length": fmt.Sprintf("%d", req.FileSize),
		},
	})
}

func validatePresignRequest(req presignRequest) error {
	if req.BucketName == "" {
		return errors.New("bucket_name is required")
	}
	if req.Filename == "" {
		return errors.New("filename is required")
	}
	if req.MIMEType == "" {
		return errors.New("mime_type is required")
	}
	if req.FileSize <= 0 {
		return errors.New("file_size must be greater than zero")
	}
	return nil
}

// buildObjectKey constructs the canonical object key for a new upload.
// Format: {media_class}/{project_id}/{year}/{month}/{ulid}.{ext}
// The original filename never appears in the key.
func buildObjectKey(mediaClass, projectID, uploadID, filename string) string {
	now := time.Now().UTC()
	ext := strings.ToLower(filepath.Ext(filename))
	return fmt.Sprintf("%s/%s/%d/%02d/%s%s",
		mediaClass, projectID, now.Year(), now.Month(), uploadID, ext,
	)
}

// buildUploadURL constructs the signed PUT URL pointing to OpenStore itself.
// The URL scheme and host are derived from the incoming request so the response
// is correct whether OpenStore is behind a proxy or accessed directly.
func buildUploadURL(r *http.Request, uploadID, token string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	host := r.Host
	return fmt.Sprintf("%s://%s/upload/%s?token=%s", scheme, host, uploadID, token)
}