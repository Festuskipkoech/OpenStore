package models
 
import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)
 
var ValidMediaClasses = map[string]bool{
	"images": true,
	"videos": true,
	"audio": true,
	"documents": true,
}
 
var ValidAccessValues = map[string]bool{
	"public": true,
	"private": true,
}

type Bucket struct {
	ID string `json:"bucket_id"`
	ProjectID string `json:"project_id"`
	Name string `json:"name"`
	MediaClass string `json:"media_class"`
	AllowedMIME []string `json:"allowed_mime"`
	MaxBytes int64 `json:"max_bytes"`
	PresignTTLSeconds int `json:"presign_ttl_seconds"`
	ReadTTLSeconds int `json:"read_ttl_seconds"`
	Access string `json:"access"`
	CreatedAt time.Time `json:"created_at"`
}
 
type CreateBucketParams struct {
	ID string
	ProjectID string
	Name string
	MediaClass string
	AllowedMIME []string
	MaxBytes int64
	PresignTTLSeconds int
	ReadTTLSeconds int
	Access string
}
 
type UpdateBucketParams struct {
	AllowedMIME []string
	MaxBytes *int64
	PresignTTLSeconds *int
	ReadTTLSeconds *int
}

func CreateBucket(db *sql.DB, p CreateBucketParams) (*Bucket, error) {
	mime, err := json.Marshal(p.AllowedMIME)
	if err != nil {
		return nil, fmt.Errorf("marshal allowed_mime: %w", err)
	}
 
	_, err = db.Exec(`
		INSERT INTO buckets (id, project_id, name, media_class, allowed_mime, max_bytes,
		presign_ttl_seconds, read_ttl_seconds, access)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.ProjectID, p.Name, p.MediaClass, string(mime),
		p.MaxBytes, p.PresignTTLSeconds, p.ReadTTLSeconds, p.Access,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("insert bucket: %w", err)
	}
 
	return GetBucketByID(db, p.ID)
}

func CreateBucketTx(tx *sql.Tx, p CreateBucketParams) (*Bucket, error) {
	mime, err := json.Marshal(p.AllowedMIME)
	if err != nil {
		return nil, fmt.Errorf("marshal allowed_mime: %w", err)
	}

	_, err = tx.Exec(`
		INSERT INTO buckets (id, project_id, name, media_class, allowed_mime, max_bytes,
		presign_ttl_seconds, read_ttl_seconds, access)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.ProjectID, p.Name, p.MediaClass, string(mime),
		p.MaxBytes, p.PresignTTLSeconds, p.ReadTTLSeconds, p.Access,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("insert bucket: %w", err)
	}
 
	return getBucketByIDTx(tx, p.ID)
}

func GetBucketByID(db *sql.DB, id string) (*Bucket, error) {
	row := db.QueryRow(`
		SELECT id, project_id, name, media_class, allowed_mime, max_bytes,
		presign_ttl_seconds, read_ttl_seconds, access, created_at
		FROM buckets WHERE id = ?`, id)
 
	return scanBucket(row)
}

func getBucketByIDTx(tx *sql.Tx, id string) (*Bucket, error) {
	row := tx.QueryRow(`
		SELECT id, project_id, name, media_class, allowed_mime, max_bytes,
		       presign_ttl_seconds, read_ttl_seconds, access, created_at
		FROM buckets WHERE id = ?`, id)
 
	return scanBucket(row)
}

func GetBucketByName(db *sql.DB, projectID, name string) (*Bucket, error) {
	row := db.QueryRow(`
		SELECT id, project_id, name, media_class, allowed_mime, max_bytes,
		       presign_ttl_seconds, read_ttl_seconds, access, created_at
		FROM buckets WHERE project_id = ? AND name = ?`, projectID, name)
 
	return scanBucket(row)
}

func GetBucketByNameTx(tx *sql.Tx, projectID, name string) (*Bucket, error) {
	row := tx.QueryRow(`
		SELECT id, project_id, name, media_class, allowed_mime, max_bytes,
		       presign_ttl_seconds, read_ttl_seconds, access, created_at
		FROM buckets WHERE project_id = ? AND name = ?`, projectID, name)

	return scanBucket(row)
}

func GetAllBucketsForProject(db *sql.DB, projectID string) ([]*Bucket, error) {
	rows, err := db.Query(`
		SELECT id, project_id, name, media_class, allowed_mime, max_bytes,
		       presign_ttl_seconds, read_ttl_seconds, access, created_at
		FROM buckets WHERE project_id = ?
		ORDER BY created_at ASC`, projectID)
	if err != nil {
		return nil, fmt.Errorf("query buckets: %w", err)
	}
	defer rows.Close()
 
	var buckets []*Bucket
	for rows.Next() {
		b, err := scanBucketRow(rows)
		if err != nil {
			return nil, err
		}
		buckets = append(buckets, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate buckets: %w", err)
	}
 
	return buckets, nil
}

func UpdateBucket(db *sql.DB, id string, p UpdateBucketParams) (*Bucket, error) {
	setClauses := []string{}
	args := []any{}
 
	if p.AllowedMIME != nil {
		mime, err := json.Marshal(p.AllowedMIME)
		if err != nil {
			return nil, fmt.Errorf("marshal allowed_mime: %w", err)
		}
		setClauses = append(setClauses, "allowed_mime = ?")
		args = append(args, string(mime))
	}
	if p.MaxBytes != nil {
		setClauses = append(setClauses, "max_bytes = ?")
		args = append(args, *p.MaxBytes)
	}
	if p.PresignTTLSeconds != nil {
		setClauses = append(setClauses, "presign_ttl_seconds = ?")
		args = append(args, *p.PresignTTLSeconds)
	}
	if p.ReadTTLSeconds != nil {
		setClauses = append(setClauses, "read_ttl_seconds = ?")
		args = append(args, *p.ReadTTLSeconds)
	}
 
	if len(setClauses) == 0 {
		return GetBucketByID(db, id)
	}
 
	args = append(args, id)
 
	_, err := db.Exec(
		"UPDATE buckets SET "+strings.Join(setClauses, ", ")+" WHERE id = ?",
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("update bucket: %w", err)
	}
 
	return GetBucketByID(db, id)
}


func UpdateBucketTx(tx *sql.Tx, id string, p UpdateBucketParams) (*Bucket, error) {
	setClauses := []string{}
	args := []any{}

	if p.AllowedMIME != nil {
		mime, err := json.Marshal(p.AllowedMIME)
		if err != nil {
			return nil, fmt.Errorf("marshal allowed_mime: %w", err)
		}
		setClauses = append(setClauses, "allowed_mime = ?")
		args = append(args, string(mime))
	}
	if p.MaxBytes != nil {
		setClauses = append(setClauses, "max_bytes = ?")
		args = append(args, *p.MaxBytes)
	}
	if p.PresignTTLSeconds != nil {
		setClauses = append(setClauses, "presign_ttl_seconds = ?")
		args = append(args, *p.PresignTTLSeconds)
	}
	if p.ReadTTLSeconds != nil {
		setClauses = append(setClauses, "read_ttl_seconds = ?")
		args = append(args, *p.ReadTTLSeconds)
	}

	if len(setClauses) == 0 {
		return getBucketByIDTx(tx, id)
	}

	args = append(args, id)

	_, err := tx.Exec(
		"UPDATE buckets SET "+strings.Join(setClauses, ", ")+" WHERE id = ?",
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("update bucket: %w", err)
	}

	return getBucketByIDTx(tx, id)
}

func DeleteBucket(db *sql.DB, id string) error {
	res, err := db.Exec("DELETE FROM buckets WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete bucket: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func BucketHasVerifiedUploads(db *sql.DB, bucketID string) (bool, error) {
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM uploads WHERE bucket_id = ? AND status = 'verified'",
		bucketID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("count verified uploads: %w", err)
	}
	return count > 0, nil
}

func scanBucket(row *sql.Row) (*Bucket, error) {
	var b Bucket
	var mimeJSON string
 
	err := row.Scan(
		&b.ID, &b.ProjectID, &b.Name, &b.MediaClass, &mimeJSON,
		&b.MaxBytes, &b.PresignTTLSeconds, &b.ReadTTLSeconds, &b.Access, &b.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan bucket: %w", err)
	}
 
	if err := json.Unmarshal([]byte(mimeJSON), &b.AllowedMIME); err != nil {
		return nil, fmt.Errorf("unmarshal allowed_mime: %w", err)
	}
 
	return &b, nil
}

func scanBucketRow(rows *sql.Rows) (*Bucket, error) {
	var b Bucket
	var mimeJSON string
	
	err := rows.Scan(
		&b.ID, &b.ProjectID, &b.Name, &b.MediaClass, &mimeJSON,
		&b.MaxBytes, &b.PresignTTLSeconds, &b.ReadTTLSeconds, &b.Access, &b.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan bucket row: %w", err)
	}
 
	if err := json.Unmarshal([]byte(mimeJSON), &b.AllowedMIME); err != nil {
		return nil, fmt.Errorf("unmarshal allowed_mime: %w", err)
	}

	return  &b, nil
}

