package handlers
 
import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
 
	"openstore/internal/models"
)

// fireWebhook delivers a signed webhook to the project's webhook_url in a goroutine.
// Does not block the upload response. Delivery failures are logged only — retry queue is phase 5.
func (h *UploadHandler) fireWebhook(upload *models.Upload, bucket *models.Bucket) {
	project, err := models.GetProject(h.db, bucket.ProjectID)
	if err != nil {
		slog.Error("webhook: load project", "upload_id", upload.ID, "error", err)
		return
	}

	var event string
	var payload webhookPayload

	if upload.Status == "verified" {
		event = "upload.verified"
		payload = webhookPayload{
			Event: event,
			UploadID: upload.ID,
			Bucket: bucket.Name,
			ObjectKey: upload.ObjectKey,
			PublicURL: upload.PublicURL,
			ContentType: upload.ContentType,
			SizeBytes: upload.SizeBytes,
			VerifiedAt: upload.VerifiedAt,
		}
	} else {
		event = "upload.rejected"
		payload = webhookPayload{
			Event: event,
			UploadID: upload.ID,
			Bucket: bucket.Name,
			ObjectKey: upload.ObjectKey,
			RejectionReason: upload.RejectionReason,
			Error: stringOrEmpty(upload.RejectionReason),
			Code: "verification_failed",
		}
	}
	body, err := json.Marshal(payload)
	if err != nil{
		slog.Error("webhook: marshal payload", "upload_id", upload.ID, "error", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, project.WebhookURL, bytes.NewReader(body))
	
	if err != nil {
		slog.Error("webhook: build request", "upload_id", upload.ID, "error", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OpenStore-Signature", "hmac-sha256="+signWebhookBody(body, project.WebhookSecret))
	req.Header.Set("X-OpenStore-Event", event)

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do((req))
	if err != nil {
		slog.Error("webhook: delivery failed", "upload_id", upload.ID, "url", project.WebhookURL, "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Warn("webhook: non-2xx response", "upload_id", upload.ID, "status", resp.StatusCode)
		return
	}
 
	slog.Info("webhook: delivered", "upload_id", upload.ID, "event", event, "status", resp.StatusCode)
}

// signWebhookBody computes HMAC-SHA256 over body using secret.
// The client backend must verify this with constant-time comparison before processing.
func signWebhookBody(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)

	return hex.EncodeToString(mac.Sum(nil))
}

// fireRejectionWebhook is a convenience wrapper for the rejection path where the upload is already marked.
func (h *UploadHandler) fireRejectionWebhook(uploadID string) {
	upload, err := models.GetUploadByID(h.db, uploadID)
	if err != nil {
		slog.Error("webhook: load rejected upload", "upload_id", uploadID, "error", err)
		return
	}
	
	bucket, err := models.GetBucketByID(h.db, upload.BucketID)
	if err != nil {
		slog.Error("webhook: load bucket for rejected upload", "upload_id", uploadID, "error", err)
		return
	}
	h.fireWebhook(upload, bucket)
}

func stringOrEmpty(s *string) string {
	if s == nil {
		return  ""
	}
	return fmt.Sprintf("%s", *s)
}