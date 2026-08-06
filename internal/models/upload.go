package models

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Upload struct {
	ID string `json:"upload_id"`
	ProjectID string `json:"project_id"`
	BucketID string `json:"bucket_id"`
	ObjectKey string `json:"object_key"`
	OriginalName string `json:"original_name"`
	ContentType string `json:"content_type"`
	SizeBytes int64 `json:"size_bytes"`
	PublicURL *string `json:"public_url"`
	Status string `json:"status"`
	RejectionReason *string `json:"rejection_reason,omitempty"`
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateUploadParams struct {
	ID string
	ProjectID string
	BucketID string
	ObjectKey string
	OriginalName string
	ContentType string
	SizeBytes int64
	ExpiresAt time.Time
}

type UpdateUploadStatusParams struct {
	Status string
	PublicURL *string
	RejectionReason *string
	VerifiedAt *time.Time
}

func CreateUpload(db *sql.DB, p CreateUploadParams) (*Upload, error) {
	_, err := db.Exec(`
		INSERT INTO uploads
			(id, project_id, bucket_id, object_key, original_name, content_type, size_bytes, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.ProjectID, p.BucketID, p.ObjectKey,
		p.OriginalName, p.ContentType, p.SizeBytes, p.ExpiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert upload: %w", err)
	}

	return GetUploadByID(db, p.ID)
}

func GetUploadByID(db *sql.DB, id string) (*Upload, error) {
	row := db.QueryRow(`
		SELECT id, project_id, bucket_id, object_key, original_name, content_type,
		       size_bytes, public_url, status, rejection_reason, verified_at,
		       expires_at, created_at
		FROM uploads
		WHERE id = ?`, id)

	return scanUpload(row)
}

func UpdateUploadStatus(db *sql.DB, id string, p UpdateUploadStatusParams) (*Upload, error) {
	_, err := db.Exec(`
		UPDATE uploads
		SET status = ?, public_url = ?, rejection_reason = ?, verified_at = ?
		WHERE id = ?`,
		p.Status, p.PublicURL, p.RejectionReason, p.VerifiedAt, id,
	)
	if err != nil {
		return nil, fmt.Errorf("update upload status: %w", err)
	}

	return GetUploadByID(db, id)
}

func GetExpiredPendingUploads(db *sql.DB) ([]*Upload, error) {
    rows, err := db.Query(`
        SELECT id, project_id, bucket_id, object_key, original_name, content_type,
               size_bytes, public_url, status, rejection_reason, verified_at,
               expires_at, created_at
        FROM uploads
        WHERE status = 'pending' AND expires_at < strftime('%Y-%m-%d %H:%M:%S', 'now')`)
    if err != nil {
        return nil, fmt.Errorf("query expired uploads: %w", err)
    }
    defer rows.Close()

    var uploads []*Upload
    for rows.Next() {
        u, err := scanUploadRow(rows)
        if err != nil {
            return nil, err
        }
        uploads = append(uploads, u)
    }
    return uploads, rows.Err()
}

func scanUpload(row *sql.Row) (*Upload, error) {
	var u Upload

	err := row.Scan(
		&u.ID, &u.ProjectID, &u.BucketID, &u.ObjectKey, &u.OriginalName,
		&u.ContentType, &u.SizeBytes, &u.PublicURL, &u.Status,
		&u.RejectionReason, &u.VerifiedAt, &u.ExpiresAt, &u.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan upload: %w", err)
	}

	return &u, nil
}

func scanUploadRow(rows *sql.Rows) (*Upload, error) {
	var u Upload
	err := rows.Scan(
		&u.ID, &u.ProjectID, &u.BucketID, &u.ObjectKey, &u.OriginalName,
		&u.ContentType, &u.SizeBytes, &u.PublicURL, &u.Status,
		&u.RejectionReason, &u.VerifiedAt, &u.ExpiresAt, &u.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan upload: %w", err)
	}
	return &u, nil
}