package models

import (
	"database/sql"
	"fmt"
	"time"
)

// UpdateUploadStatusTx updates upload status fields inside an existing transaction.
// Used by the stream handler to make quota deduction and verified status atomic in one commit.
func UpdateUploadStatusTx(tx *sql.Tx, id string, p UpdateUploadStatusParams) (*Upload, error) {
	_, err := tx.Exec(`
		UPDATE uploads
		SET status = ?, public_url = ?, rejection_reason = ?, verified_at = ?
		WHERE id = ?`,
		p.Status, p.PublicURL, p.RejectionReason, p.VerifiedAt, id,
	)
	if err != nil {
		return nil, fmt.Errorf("update upload status (tx): %w", err)
	}
	return getUploadByIDTx(tx, id)
}

func getUploadByIDTx(tx *sql.Tx, id string) (*Upload, error) {
	row := tx.QueryRow(`
		SELECT id, project_id, bucket_id, object_key, original_name, content_type,
		       size_bytes, public_url, status, rejection_reason, verified_at,
		       expires_at, created_at
		FROM uploads WHERE id = ?`, id)

	var u Upload
	err := row.Scan(
		&u.ID, &u.ProjectID, &u.BucketID, &u.ObjectKey, &u.OriginalName,
		&u.ContentType, &u.SizeBytes, &u.PublicURL, &u.Status,
		&u.RejectionReason, &u.VerifiedAt, &u.ExpiresAt, &u.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan upload (tx): %w", err)
	}
	return &u, nil
}

func MarkUploadDeletedTx(tx *sql.Tx, id string) error {
    _, err := tx.Exec(`
        UPDATE uploads SET status = 'deleted', public_url = NULL
        WHERE id = ?`, id)
    if err != nil {
        return fmt.Errorf("mark upload deleted (tx): %w", err)
    }
    return nil
}

// MarkUploadDeleted sets status to deleted and clears public_url. Called by DELETE /files/{upload_id} in phase 6.
func MarkUploadDeleted(db *sql.DB, id string) (*Upload, error) {
	now := time.Now().UTC()
	return UpdateUploadStatus(db, id, UpdateUploadStatusParams{
		Status:     "deleted",
		VerifiedAt: &now,
	})
}