package handlers

import (
	"io"
	"time"
)
// countingLimitedReader counts bytes read and returns errLimitExceeded once the limit is crossed.
type countingLimitedReader struct {
	r io.Reader
	limit int64
	bytesRead int64
}

type uploadVerifiedResponse struct {
	UploadID string `json:"upload_id"`
	Status string `json:"status"`
	ObjectKey string `json:"object_key"`
	Bucket string `json:"bucket"`
	PublicURL *string `json:"public_url"`
	ContentType string `json:"content_type"`
	SizeBytes int64 `json:"size_bytes"`
	VerifiedAt  *time.Time `json:"verified_at"`
}

type webhookPayload struct {
	Event string `json:"event"`
	UploadID string `json:"upload_id"`
	Bucket string `json:"bucket"`
	ObjectKey string `json:"object_key"`
	PublicURL *string `json:"public_url,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	SizeBytes int64 `json:"size_bytes,omitempty"`
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
	RejectionReason *string `json:"rejection_reason,omitempty"`
	Error string `json:"error,omitempty"`
	Code string `json:"code,omitempty"`
}