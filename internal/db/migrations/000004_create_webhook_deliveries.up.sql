CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id           TEXT PRIMARY KEY,
    upload_id    TEXT NOT NULL REFERENCES uploads(id) ON DELETE CASCADE,
    project_id   TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    url          TEXT NOT NULL,
    status_code  INTEGER,
    attempt      INTEGER NOT NULL DEFAULT 1,
    succeeded    INTEGER NOT NULL DEFAULT 0,
    error        TEXT,
    delivered_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_upload_id ON webhook_deliveries(upload_id);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_project_id ON webhook_deliveries(project_id);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_pending ON webhook_deliveries(succeeded, attempt) WHERE succeeded = 0;
