package quota

import (
	"database/sql"
	"errors"
	"fmt"
)

var ErrQuotaExceeded = errors.New("file size would exceed project quota")

// CheckHeadroom returns ErrQuotaExceeded if adding fileSize bytes would exceed
// the project's quota_bytes ceiling. A quota_bytes of 0 means unlimited.
func CheckHeadroom(db *sql.DB, projectID string, fileSize int64) error {
	var quotaBytes, usedBytes int64

	err := db.QueryRow(`
		SELECT quota_bytes, used_bytes
		FROM projects
		WHERE id = ?`, projectID,
	).Scan(&quotaBytes, &usedBytes)
	if err != nil {
		return fmt.Errorf("read project quota: %w", err)
	}

	if quotaBytes == 0 {
		return nil
	}

	if usedBytes+fileSize > quotaBytes {
		return ErrQuotaExceeded
	}

	return nil
}

// Deduct increments used_bytes inside an existing transaction. Called after a verified upload.
func Deduct(tx *sql.Tx, projectID string, bytes int64) error {
	_, err := tx.Exec(`
		UPDATE projects
		SET used_bytes = used_bytes + ?
		WHERE id = ?`, bytes, projectID,
	)
	if err != nil {
		return fmt.Errorf("deduct quota: %w", err)
	}
	return nil
}

// Release decrements used_bytes inside an existing transaction. Called after a verified upload is deleted.
// Floors at zero — used_bytes never goes negative.
func Release(tx *sql.Tx, projectID string, bytes int64) error {
	_, err := tx.Exec(`
		UPDATE projects
		SET used_bytes = MAX(0, used_bytes - ?)
		WHERE id = ?`, bytes, projectID,
	)
	if err != nil {
		return fmt.Errorf("release quota: %w", err)
	}
	return nil
}