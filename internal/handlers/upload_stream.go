package handlers
 
import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
 
	"github.com/go-chi/chi/v5"
 
	"openstore/internal/models"
	"openstore/internal/quota"
	"openstore/internal/security"
)

// Stream handles PUT /upload/{uploadID}.
// Browser-facing — authenticates via HMAC token in the query string, not the API key.
// This route must live outside the Auth middleware group in main.go.
func (h *UploadHandler) Stream(w http.ResponseWriter, r *http.Request) {
	uploadID := chi.URLParam(r, "uploadID")
	if uploadID == "" {
		writeError(w, http.StatusBadRequest, "missing upload_id", "invalid_request")
		return
	}
	
	// Step 1 — Token validation.
	rawToken := r.URL.Query().Get("token")

	if rawToken == "" {
		writeError(w, http.StatusUnauthorized, "missing upload token", "unauthorized")
		return
	}

	claims, err := h.tokenizer.Verify(rawToken)
	if err != nil {
		switch err {
		case security.ErrTokenExpired:
			writeError(w, http.StatusUnauthorized, "upload token has expired", "unauthorized")
		default:
			writeError(w, http.StatusUnauthorized, "invalid upload token", "unauthorized")
		}
		return
	}

	if claims.UploadID != uploadID {
		writeError(w, http.StatusUnauthorized, "token upload_id mismatch", "unauthorized")
		return
	}

	// Step 2 — Load upload record and ownership check.
	upload, err := models.GetUploadByID(h.db, uploadID)
	if err != nil {
		if err == models.ErrNotFound {
			// 404 not 403 — avoids leaking existence of other projects' upload IDs.
			writeError(w, http.StatusNotFound, "upload not found", "not_found")
			return
		}
		slog.Error("get upload", "upload_id", uploadID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}

	bucket, err := models.GetBucketByID(h.db, upload.BucketID)
	if err != nil {
		slog.Error("get bucket for upload", "bucket_id", upload.BucketID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}

	if bucket.Name != claims.BucketName {
		writeError(w, http.StatusUnauthorized, "token bucket_name mismatch", "unauthorized")
		return
	}

	// Step 3 — Status gate: must be pending to prevent replay.
	if upload.Status != "pending" {
		writeError(w, http.StatusConflict, "upload already processed", "invalid_request")
		return
	}

	contentType := r.Header.Get("Content-Type")
		
	if contentType == "" {
		writeError(w, http.StatusBadRequest, "Content-Type header is required", "invalid_request")
		return
	}
	if mimeOnly(contentType) != claims.MIMEType {
		writeError(w, http.StatusBadRequest, "Content-Type does not match token", "invalid_request")
		return
	}
	if r.ContentLength > bucket.MaxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file size exceeds bucket limit", "size_exceeded")
		return
	}
	// removed —since size is enforced by the countingLimitedReader during streaming
	// if r.ContentLength > 0 && r.ContentLength != claims.FileSize {
	// 	writeError(w, http.StatusBadRequest, "Content-Length does not match token", "invalid_request")
	// 	return
	// }

	// Steps 5 + 6 — Stream to SeaweedFS while tee-ing(splitting the first HeaderSize bytes for magic byte verification.
	headerBuf := make([]byte, security.HeaderSize)
	headerFilled := 0

	limitedBody := &countingLimitedReader{r:r.Body, limit: bucket.MaxBytes}
	teeBody := io.TeeReader(limitedBody, &headerWriter{buf: headerBuf, filled: &headerFilled})

	ctx := r.Context()
	writeErr := h.seaweed.WriteObject(ctx, upload.ObjectKey, claims.MIMEType, teeBody)
	bytesReceived := limitedBody.bytesRead

	if writeErr != nil {
		_ = h.seaweed.DeleteObject(ctx, upload.ObjectKey)
		if isLimitExceeded(writeErr) {
			rejectUpload(h.db, upload.ID, "file size exceeds bucke size")
			writeError(w, http.StatusRequestEntityTooLarge, "file size exceeds bucket limit", "size_exceeded")
			return
		}
		slog.Error("stream to seaweedfs", "upload_id", uploadID, "error", writeErr)
		rejectUpload(h.db, upload.ID, "storage write failed")
		writeError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}

	// Step 7 — Magic byte verification.
	if headerFilled < security.HeaderSize {
		_ = h.seaweed.DeleteObject(ctx, upload.ObjectKey)
		rejectUpload(h.db, upload.ID, "file too small for magic byte verification")
		writeError(w, http.StatusUnprocessableEntity, "file too small for magic byte verification", "verification_failed")
		return
	}

	if err := security.VerifyMagicBytes(claims.MIMEType, headerBuf); err != nil {
		_ = h.seaweed.DeleteObject(ctx, upload.ObjectKey)
		rejectUpload(h.db, upload.ID, err.Error())
		writeErrorWithCode(w, http.StatusUnprocessableEntity, err.Error(), "verification_failed")
		return
	}
	// Step 8 — ClamAV antivirus scan. Fail-closed: unreachable daemon rejects the upload.
	if h.cfg.ClamAVEnabled {
		if err := scanWithClamAV(ctx, h.cfg.ClamAVURL, upload.ObjectKey, h.seaweed); err != nil {
			_= h.seaweed.DeleteObject(ctx, upload.ObjectKey)
			reason := fmt.Sprintf("antivirus: %s", err.Error())
			writeErrorWithCode(w, http.StatusUnprocessableEntity, reason, "verification_failed")
			return
		}
	}

	// Step 9 — Deep content inspection (images: govips re-encode; PDFs: pdfcpu scan).
	if err := deepInspect(ctx, h.db, h.seaweed, upload, bucket, claims.MIMEType); err != nil {
		_ = h.seaweed.DeleteObject(ctx, upload.ObjectKey)
		reason := fmt.Sprintf("deep inspection: %s", err.Error())
		rejectUpload(h.db, upload.ID, reason)
		writeErrorWithCode(w, http.StatusUnprocessableEntity, reason, "verification_failed")
		return
	}
	// Step 10 — Quota deduction and verified status in a single atomic transaction.
	tx, err := h.db.Begin()
	if err != nil {
		slog.Error("begin transaction", "upload_id", uploadID, "error", err)
		_ = h.seaweed.DeleteObject(ctx, upload.ObjectKey)
		rejectUpload(h.db, upload.ID, "internal error")
		writeError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	defer tx.Rollback()

	if err := quota.Deduct(tx, bucket.ProjectID, bytesReceived); err != nil {
		slog.Error("quota deduction", "upload_id", uploadID, "error", err)
		_ = h.seaweed.DeleteObject(ctx, upload.ObjectKey)
		rejectUpload(h.db, upload.ID, "quota deduction failed")
		writeError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}

	now := time.Now().UTC()
	params := models.UpdateUploadStatusParams{Status: "verified", VerifiedAt: &now}
	if bucket.Access == "public" {
		publicUrl := h.seaweed.PublicURL(upload.ObjectKey)
		params.PublicURL = &publicUrl
	}

	if _, err := models.UpdateUploadStatusTx(tx, upload.ID, params); err != nil {
		slog.Error("mark upload verified", "upload_id", uploadID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	
	if err := tx.Commit(); err != nil {
		slog.Error("commit verification transaction", "upload_id", uploadID, "error", err)
		_ = h.seaweed.DeleteObject(ctx, upload.ObjectKey)
		writeError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	verified, err := models.GetUploadByID(h.db, upload.ID)
	if err != nil {
		slog.Error("reload verified upload", "upload_id", uploadID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}

	slog.Info("upload verified", "upload_id", uploadID, "bucket", bucket.Name, "size_bytes", bytesReceived)

	go h.fireWebhook(verified, bucket)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(verifiedResponse(verified))
}

// scanWithClamAV streams the object into ClamAV over TCP using the INSTREAM protocol.
// Returns an error if a threat is found or the daemon is unreachable — fail-closed by design.
func scanWithClamAV(ctx context.Context, clamavURL, objectKey string, sw SeaweedClient) error {
	addr := strings.TrimPrefix(clamavURL, "tcp://")
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("antivirus_unavailable: cannot connect to ClamAV at %s: %w", addr, err)
	}

	defer conn.Close()

	if _, err := fmt.Fprintf(conn, "zINSTREAM\000"); err != nil {
		return fmt.Errorf("antivirus_unavailable: send INSTREAM command: %w", err)
	}

	var buf bytes.Buffer
	if err := sw.ReadObject(ctx, objectKey, &buf); err != nil {
		return fmt.Errorf("antivirus read object: %w", err)
	}

	data := buf.Bytes()
	size := uint32(len(data))
	lenBuf := []byte{byte(size >> 24), byte(size >> 16), byte(size >> 8), byte(size)}
	if _, err := conn.Write(append(lenBuf, data...)); err != nil {
		return fmt.Errorf("antivirus send data: %w", err)
	}
	if _, err := conn.Write([]byte{0, 0, 0, 0}); err != nil {
		return fmt.Errorf("antivirus send terminator: %w", err)
	}
	respBuf := make([]byte, 256)
	n, err := conn.Read(respBuf)
	
	if err != nil && err != io.EOF {
		return fmt.Errorf("antivirus read response: %w", err)
	}
	response := strings.TrimSpace(string(respBuf[:n]))
	if strings.HasSuffix(response, "OK") {
		return  nil
	}

	return fmt.Errorf("%s", response)
}

// deepInspect dispatches to the correct inspector by media class.
// Images are re-encoded via govips (strips all metadata). PDFs are scanned by pdfcpu.
// Videos and audio pass through — ClamAV is sufficient for those.
func deepInspect(ctx context.Context, db *sql.DB, sw SeaweedClient, upload *models.Upload, bucket *models.Bucket, mimeType string) error {
	switch bucket.MediaClass {
	case "images":
		return inspectImage(ctx, sw, upload, mimeType)
	case "documents":
		return inspectPDF(ctx, sw, upload)
	default:
		return nil
	}
}

// inspectImage re-encodes via govips to strip all EXIF, ICC, XMP, and comments.
// The sanitised image replaces the original in SeaweedFS.
func inspectImage(ctx context.Context, sw SeaweedClient, upload *models.Upload, mimeType string) error {
	var buf bytes.Buffer
	if err := sw.ReadObject(ctx, upload.ObjectKey, &buf); err != nil {
		return fmt.Errorf("read image for inspection: %w", err)
	}
	sanitised, err := sanitiseImage(buf.Bytes(), mimeType)
	if err != nil {
		return fmt.Errorf("image re-encode failed: %w", err)
	}
	return sw.WriteObject(ctx, upload.ObjectKey, mimeType, bytes.NewReader(sanitised))
}


// inspectPDF scans for embedded JavaScript, executables, URI open actions, and launch actions via pdfcpu.
// OpenStore rejects rather than sanitises — a PDF with these elements is suspect.
func inspectPDF(ctx context.Context, sw SeaweedClient, upload *models.Upload) error {
	var buf bytes.Buffer
	if err := sw.ReadObject(ctx, upload.ObjectKey, &buf); err != nil {
		return fmt.Errorf("read pdf for inspection: %w", err)
	}
	return scanPDFForDangerousElements(buf.Bytes())
}
 
// rejectUpload marks the upload rejected with a reason. Called on every failure path.
func rejectUpload(db *sql.DB, uploadID, reason string) {
	if _, err := models.UpdateUploadStatus(db, uploadID, models.UpdateUploadStatusParams{
		Status: "rejected",
		RejectionReason: &reason,
	}); err != nil {
		slog.Error("mark upload rejected", "upload_id", uploadID, "reason", reason, "error", err)
	}
}

func verifiedResponse(u *models.Upload) uploadVerifiedResponse {
	return uploadVerifiedResponse{
		UploadID: u.ID,
		Status: u.Status,
		ObjectKey: u.ObjectKey,
		PublicURL: u.PublicURL,
		ContentType: u.ContentType,
		SizeBytes: u.SizeBytes,
		VerifiedAt: u.VerifiedAt,
	}
}

