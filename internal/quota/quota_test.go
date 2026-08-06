package quota_test

import (
    "database/sql"
    "path/filepath"
    "testing"

    "openstore/internal/db"
    "openstore/internal/quota"
)

func openTestDB(t *testing.T) *sql.DB {
    t.Helper()
    path := filepath.Join(t.TempDir(), "test.db")
    database, err := db.Open(path)
    if err != nil {
        t.Fatalf("open test db: %v", err)
    }
    t.Cleanup(func() { database.Close() })
    return database
}

func insertProject(t *testing.T, database *sql.DB, quotaBytes, usedBytes int64) string {
	t.Helper()
	id := "proj-test-" + t.Name()
	_, err := database.Exec(`
		INSERT INTO projects (id, name, api_key_hash, webhook_url, webhook_secret, allowed_origins, quota_bytes, used_bytes)
		VALUES (?, 'test', '', 'http://x.com', 'secret', '[]', ?, ?)`,
		id, quotaBytes, usedBytes,
	)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}
	return id
}

func TestCheckHeadroom_FitsWithinQuota(t *testing.T) {
	database := openTestDB(t)
	projectID := insertProject(t, database, 10*1024*1024*1024, 1*1024*1024*1024)
	if err := quota.CheckHeadroom(database, projectID, 500*1024*1024); err != nil {
		t.Errorf("expected pass, got: %v", err)
	}
}

func TestCheckHeadroom_ExactlyAtLimit(t *testing.T) {
	database := openTestDB(t)
	quota_bytes := int64(10 * 1024 * 1024 * 1024)
	used := int64(9.5 * 1024 * 1024 * 1024)
	projectID := insertProject(t, database, quota_bytes, used)
	// 9.5GB used + 512MB = exactly 10GB — should pass
	if err := quota.CheckHeadroom(database, projectID, 512*1024*1024); err != nil {
		t.Errorf("expected exactly at limit to pass, got: %v", err)
	}
}

func TestCheckHeadroom_ExceedsByOneByte(t *testing.T) {
	database := openTestDB(t)
	quota_bytes := int64(10 * 1024 * 1024 * 1024)
	projectID := insertProject(t, database, quota_bytes, quota_bytes-1)
	if err := quota.CheckHeadroom(database, projectID, 2); err == nil {
		t.Error("expected quota exceeded, got nil")
	}
}

func TestCheckHeadroom_UnlimitedQuota(t *testing.T) {
	database := openTestDB(t)
	projectID := insertProject(t, database, 0, 999*1024*1024*1024)
	if err := quota.CheckHeadroom(database, projectID, 999*1024*1024*1024); err != nil {
		t.Errorf("expected unlimited quota to pass, got: %v", err)
	}
}

func TestCheckHeadroom_ZeroFileSize(t *testing.T) {
	database := openTestDB(t)
	projectID := insertProject(t, database, 10*1024*1024*1024, 5*1024*1024*1024)
	if err := quota.CheckHeadroom(database, projectID, 0); err != nil {
		t.Errorf("expected zero file size to pass, got: %v", err)
	}
}
