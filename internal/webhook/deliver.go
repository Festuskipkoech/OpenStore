package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/oklog/ulid/v2"

	"openstore/internal/models"
)

// verifiedPayload is the webhook body for a successful upload.
type verifiedPayload struct {
	Event string `json:"event"`
	UploadID string `json:"upload_id"`
	ProjectID string `json:"project_id"`
	ObjectKey string `json:"object_key"`
	Bucket string `json:"bucket"`
	Access string `json:"access"`
	PublicURL *string `json:"public_url"`
	ContentType string `json:"content_type"`
	SizeBytes int64 `json:"size_bytes"`
	VerifiedAt *time.Time `json:"verified_at"`
}

// rejectedPayload is the webhook body for a failed upload.
type rejectedPayload struct {
	Event string `json:"event"`
	UploadID string `json:"upload_id"`
	ProjectID string `json:"project_id"`
	RejectionReason *string `json:"rejection_reason"`
	RejectedAt *time.Time `json:"rejected_at"`
}

var httpClient = &http.Client{Timeout: 10 *time.Second}
 
// Deliver fires the webhook for an upload outcome and records the attempt.
// Must be called in a goroutine — does not block the upload response
func Deliver(db *sql.DB, project *models.Project, upload *models.Upload, bucket *models.Bucket) {
	body, err := buildPayload(upload, bucket)

	if err != nil {
		slog.Error("webhook: build payload", "upload_id", upload.ID, "error", err)
		return
	}

	deliveryID := ulid.Make().String()

	if _, err := models.CreateDelivery(db, models.CreateDeliveryParams{
		ID: deliveryID,
		UploadID: upload.ID,
		ProjectID: project.ID,
		URL: project.WebhookURL,
	}); err != nil {
		slog.Error("webhook: create delivery record", "upload_id", upload.ID, "error", err)
		return
	}

	attempt(db, deliveryID, project, body)
}

// StartRetryWorker polls every 60 seconds for undelivered webhooks and retries them.
// Started once from main.go as a goroutine.
func StartRetryWorker(ctx context.Context, db *sql.DB) {
	ticker := time.NewTicker(60 * time.Second)

	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			RetryPending(db)
		}
	}
}

// retryPending finds all eligible pending deliveries and retries each one.
func RetryPending(db *sql.DB) {
	deliveries, err := models.GetPendingDeliveries(db)
	if err != nil {
		slog.Error("webhook retry: query pending", "error", err)
		return
	}

	for _, d := range deliveries {
		upload, err := models.GetUploadByID(db, d.UploadID)

		if err != nil {
			slog.Error("webhook retry: load upload", "delivery_id", d.ID, "error", err)
			continue
		}
 
		project, err := models.GetProject(db, d.ProjectID)
		if err != nil {
			slog.Error("webhook retry: load project", "delivery_id", d.ID, "error", err)
			continue
		}
 
		bucket, err := models.GetBucketByID(db, upload.BucketID)
		if err != nil {
			slog.Error("webhook retry: load bucket", "delivery_id", d.ID, "error", err)
			continue
		}
 
		body, err := buildPayload(upload, bucket)
		if err != nil {
			slog.Error("webhook retry: build payload", "delivery_id", d.ID, "error", err)
			continue
		}
 
		if err := models.IncrementAttempt(db, d.ID); err != nil {
			slog.Error("webhook retry: increment attempt", "delivery_id", d.ID, "error", err)
			continue
		}
		attempt(db, d.ID, project, body)
	}
}

// attempt executes the HTTP POST and records the outcome.
func attempt(db *sql.DB, deliveryID string, project *models.Project, body []byte)  {
	req, err := http.NewRequest(http.MethodPost, project.WebhookURL, bytes.NewReader(body))
	if err != nil {
		errMsg := fmt.Sprintf("build request: %s", err)
		_ = models.UpdateDelivery(db, deliveryID, nil, false, &errMsg)
		slog.Error("webhook: build request", "delivery_id", deliveryID, "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OpenStore-Signature", "hmac-sha256="+sign(body, project.WebhookSecret))

	resp, err := httpClient.Do(req)

	if err != nil {
		errMsg := err.Error()
		_ = models.UpdateDelivery(db, deliveryID, nil, false, &errMsg)
		slog.Warn("webhook: delivery failed", "delivery_id", deliveryID, "url", project.WebhookURL, "error", err)
		return
	}
	defer resp.Body.Close()

	code := resp.StatusCode
	succeeded := code >= 200 && code < 300

	var errMsg *string
	if !succeeded {
		msg := fmt.Sprintf("non-2xx response: %d", code)
		errMsg = &msg
	}

	_ = models.UpdateDelivery(db, deliveryID, &code, succeeded, errMsg)
	if succeeded {
		slog.Info("webhook: delivered", "delivery_id", deliveryID, "status", code)
	} else {
		slog.Warn("webhook: non-2xx", "delivery_id", deliveryID, "status", code)
	}
}

// buildPayload marshals the correct payload shape for the upload status.
func buildPayload(upload *models.Upload, bucket *models.Bucket) ([]byte, error) {
	if upload.Status == "verified" {
		return json.Marshal(verifiedPayload{
			Event: "upload.verified",
			UploadID: upload.ID,
			ProjectID: upload.ProjectID,
			ObjectKey: upload.ObjectKey,
			Bucket: bucket.Name,
			Access: bucket.Access,
			PublicURL: upload.PublicURL,
			ContentType: upload.ContentType,
			SizeBytes: upload.SizeBytes,
			VerifiedAt: upload.VerifiedAt,
		})
	}
 
	return json.Marshal(rejectedPayload{
		Event: "upload.rejected",
		UploadID: upload.ID,
		ProjectID: upload.ProjectID,
		RejectionReason: upload.RejectionReason,
		RejectedAt: upload.VerifiedAt,
	})
}

// sign computes HMAC-SHA256 over body using secret.
func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return  hex.EncodeToString(mac.Sum(nil))
}