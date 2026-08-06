CREATE TABLE IF NOT EXISTS buckets (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    media_class TEXT NOT NULL,
    allowed_mime TEXT NOT NULL,
    max_bytes INTEGER NOT NULL,
    presign_ttl_seconds INTEGER NOT NULL DEFAULT 300,
    read_ttl_seconds INTEGER NOT NULL DEFAULT 900,
    access TEXT NOT NULL DEFAULT 'public',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(project_id, name)
);

CREATE INDEX IF NOT EXISTS idx_buckets_project_id ON buckets(project_id);