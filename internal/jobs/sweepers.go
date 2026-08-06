package jobs
 
import (
	"context"
	"database/sql"
	"log/slog"
	"time"
 
	"openstore/internal/models"
)

// SeaweedDeleter is satisfied by *seaweedfs.Client.
// Scoped to what the sweeper actually needs — no read or write operations.

type SeaweedDeleter interface {
	 DeleteObject(ctx context.Context, objectKey string) error
}

// StartSweeper sweeps expired pending uploads immediately on boot, then every 10 minutes.
// Exits cleanly when ctx is cancelled — call cancel() before srv.Shutdown in main.
func StartSweeper(ctx context.Context, db *sql.DB, seaweed SeaweedDeleter) {
	Sweep(ctx, db, seaweed)

	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			Sweep(ctx, db, seaweed)
		}
	}
}

// sweep finds all pending uploads past their expiry, deletes their objects from
// SeaweedFS, and marks them rejected. Each upload is handled independently —
// a failure on one does not abort the rest.
func Sweep(ctx context.Context, db *sql.DB, seaweed SeaweedDeleter) {
	uploads, err := models.GetExpiredPendingUploads(db)
	if err != nil {
		slog.Error("sweeper: query expired uploads", "error", err)
		return
	}
	if len(uploads) == 0 {
		return
	}

	slog.Error("sweeper: query expired uploads", "error", err)

	for _, u := range uploads {
		if err := seaweed.DeleteObject(ctx, u.ObjectKey); err != nil {
			// not-found is treated as success inside DeleteObject — any error here is genuine
			slog.Error("sweeper: delete object", "upload_id", u.ID, "object_key", u.ObjectKey, "error", err)
		}
		reason := "expired"
		if _, err := models.UpdateUploadStatus(db, u.ID, models.UpdateUploadStatusParams{
			Status: "rejected",
			RejectionReason: &reason,
		}); err != nil {
			slog.Error("sweeper: mark rejected", "upload_id", u.ID, "error", err)
			continue
		}
		slog.Info("sweeper: expired upload cleaned", "upload_id", u.ID, "object_key", u.ObjectKey)
	}
}
