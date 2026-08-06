package handlers

import (
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"openstore/internal/config"
	"openstore/internal/models"
	"openstore/internal/quota"
	"openstore/internal/security"
)

// FilesHandler serves file reads, read presigning, status checks, and deletion.
type FilesHandler struct {
	db *sql.DB
	seaweed SeaweedClient
	tokenizer *security.Tokenizer
	cfg *config.Config
}

type presignReadResponse struct {
	UploadID string `json:"upload_id"`
	ReadURL string `json:"read_url"`
	ExpiresAt time.Time `json:"expires_at"`
}

func NewFilesHandler(db *sql.DB, seaweed SeaweedClient, tokenizer *security.Tokenizer, cfg *config.Config) *FilesHandler {
	return &FilesHandler{db: db, seaweed: seaweed, tokenizer: tokenizer, cfg:cfg}
}

// GetUploadStatus handles GET /uploads/{uploadID}.
// Returns the current state of an upload record. Useful for polling when webhook delivery is delayed.
func (h *FilesHandler)  GetUploadStatus(w http.ResponseWriter, r *http.Request) {
	uploadID := chi.URLParam(r, "uploadID")

	upload, err := models.GetUploadByID(h.db, uploadID)
	if err != nil {
		if err == models.ErrNotFound {
			writeError(w, http.StatusNotFound, "upload not found", "not_found")
			return
		}
		slog.Error("get upload status", "upload_id", uploadID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, upload)
}

// ReadFile handles GET /files/{uploadID}.
// Public buckets stream directly. Private buckets require a signed read token in the query string.
func (h *FilesHandler) ReadFile(w http.ResponseWriter, r *http.Request) {
	uploadID := chi.URLParam(r, "uploadID")

	upload,err := models.GetUploadByID(h.db, uploadID)

	if err != nil {
		if err == models.ErrNotFound {
			writeError(w, http.StatusNotFound, "upload not found", "not_found")
			return
		}
		slog.Error("read file: get upload", "upload_id", uploadID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
 
	if upload.Status != "verified" {
		writeError(w, http.StatusNotFound, "upload not found", "not_found")
		return
	}

	bucket, err := models.GetBucketByID(h.db, upload.BucketID)
	if err != nil {
		slog.Error("read file: get bucket", "upload_id", uploadID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}

	// Private buckets require a valid signed read token.
	if bucket.Access == "private" {
		rawToken := r.URL.Query().Get("token")
		if rawToken == "" {
			writeError(w, http.StatusUnauthorized, "missing read token", "unauthorized")
			return
		}

		claims, err := h.tokenizer.VerifyRead(rawToken)
		if err != nil {
			switch err {
			case security.ErrTokenExpired:
				writeError(w, http.StatusUnauthorized, "read token has expired", "unauthorized")
			default:
				writeError(w, http.StatusUnauthorized, "invalid read token", "unauthorized")
			}
			return			
		}

		// Token must have been issued for this exact upload.
		if claims.UploadID != uploadID {
			writeError(w, http.StatusUnauthorized, "token upload_id mismatch", "unauthorized")
			return
		}
	}
	w.Header().Set("Content-Type", upload.ContentType)
	w.WriteHeader(http.StatusOK)
 
	if err := h.seaweed.ReadObject(r.Context(), upload.ObjectKey, w); err != nil {
		// Headers already sent — log only, cannot write an error response.
		slog.Error("read file: stream from seaweedfs", "upload_id", uploadID, "error", err)
	}
}

// PresignRead handles POST /files/{uploadID}/read-presign.
// Generates a short-lived signed read URL for a private file.
// Called by the client backend — requires API key.
func (h *FilesHandler) PresignRead(w http.ResponseWriter, r *http.Request) {
	uploadID := chi.URLParam(r, "uploadID")

	upload, err := models.GetUploadByID(h.db, uploadID)
	if err != nil {
		if err == models.ErrNotFound {
			writeError(w, http.StatusNotFound, "upload not found", "not_found")
			return
		}
		slog.Error("presign read: get upload", "upload_id", uploadID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
 
	if upload.Status != "verified" {
		writeError(w, http.StatusNotFound, "upload not found", "not_found")
		return
	}

	bucket, err := models.GetBucketByID(h.db, upload.BucketID)
	if err != nil {
		slog.Error("presign read: get bucket", "upload_id", uploadID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}

	if bucket.Access != "private" {
		writeError(w, http.StatusBadRequest, "read presigning is only available for private buckets", "invalid_request")
		return
	}

	ttl := time.Duration(bucket.ReadTTLSeconds) * time.Second
	expiresAt := time.Now().UTC().Add(ttl)

	token := h.tokenizer.SignRead(security.ReadTokenClaims{
		UploadID: uploadID,
		ExpiresAt: expiresAt,
	})

	readURL := h.cfg.PublicBaseURL + "/files/" + uploadID + "?token=" + token
	 
	writeJSON(w, http.StatusOK, presignReadResponse{
		UploadID: uploadID,
		ReadURL: readURL,
		ExpiresAt: expiresAt,
	})
}

// DeleteFile handles DELETE /files/{uploadID}.
// Removes the object from SeaweedFS, releases quota, and marks the record deleted.
func (h *FilesHandler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	uploadID := chi.URLParam(r, "uploadID")
	upload, err := models.GetUploadByID(h.db, uploadID)
	if err != nil {
		if err == models.ErrNotFound {
			writeError(w, http.StatusNotFound, "upload not found", "not_found")
			return
		}
		slog.Error("delete file: get upload", "upload_id", uploadID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
 
	if upload.Status != "verified" {
		writeError(w, http.StatusNotFound, "upload not found", "not_found")
		return
	}
	if err := h.seaweed.DeleteObject(r.Context(), upload.ObjectKey); err != nil {
		slog.Error("delete file: seaweedfs delete", "upload_id", uploadID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}

	tx, err := h.db.Begin()
	if err != nil {
		slog.Error("delete file: begin transaction", "upload_id", uploadID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	defer tx.Rollback()
	
	if err := quota.Release(tx, upload.ProjectID, upload.SizeBytes); err != nil {
		slog.Error("delete file: release quota", "upload_id", uploadID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}

	if err := models.MarkUploadDeletedTx(tx, uploadID); err != nil {
		slog.Error("delete file: mark deleted", "upload_id", uploadID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}

	if err := tx.Commit(); err != nil {
		slog.Error("delete file: commit", "upload_id", uploadID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}

	slog.Info("file deleted", "upload_id", uploadID, "size_bytes", upload.SizeBytes)
 
	writeJSON(w, http.StatusOK, map[string]any{
		"upload_id": uploadID,
		"deleted": true,
	})
}