CREATE TABLE IF NOT EXISTS uploads (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    bucket_id TEXT NOT NULL REFERENCES buckets(id) ON DELETE CASCADE,
    object_key TEXT NOT NULL,
    original_name TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size_bytes  INTEGER NOT NULL DEFAULT 0,
    public_url TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    rejection_reason TEXT,
    verified_at DATETIME,
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(project_id, object_key)
);

CREATE INDEX IF NOT EXISTS idx_uploads_project_status ON uploads(project_id, status);
CREATE INDEX IF NOT EXISTS idx_uploads_expires ON uploads(expires_at);
CREATE INDEX IF NOT EXISTS idx_uploads_bucket_id ON uploads(bucket_id);