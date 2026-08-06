package models

import "database/sql"

// GetBucketByNameForPresign looks up a bucket by its name without requiring a
// project_id. Used by the presign handler which receives only a bucket name
// from the client — the project_id is read from the bucket record itself and
// used for all subsequent quota and ownership checks.
func GetBucketByNameForPresign(db *sql.DB, name string) (*Bucket, error) {
	row := db.QueryRow(`
		SELECT id, project_id, name, media_class, allowed_mime, max_bytes,
		       presign_ttl_seconds, read_ttl_seconds, access, created_at
		FROM buckets
		WHERE name = ?`, name)

	return scanBucket(row)
}