package models
 
import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type WebhookDelivery struct {
	ID string
	UploadID string
	ProjectID string
	URL string
	StatusCode *int
	Attempt int
	Succeeded bool
	Error *string
	DeliveredAt time.Time
}
 
type CreateDeliveryParams struct {
	ID string
	UploadID string
	ProjectID string
	URL string
}

// CreateDelivery inserts the first attempt record before the HTTP call is made.
func CreateDelivery(db *sql.DB, p CreateDeliveryParams) (*WebhookDelivery, error) {
	_, err := db.Exec(`
		INSERT INTO webhook_deliveries (id, upload_id, project_id, url)
		VALUES (?, ?, ?, ?)`,
		p.ID, p.UploadID, p.ProjectID, p.URL,
	)
	if err != nil {
		return nil, fmt.Errorf("insert webhook_delivery: %w", err)
	}
	return GetDeliveryByID(db, p.ID)
}

// UpdateDelivery records the outcome of an attempt.
func UpdateDelivery(db *sql.DB, id string, statusCode *int, succeeded bool, errMsg *string) error {
	_, err := db.Exec(`
		UPDATE webhook_deliveries
		SET status_code = ?, succeeded = ?, error = ?, delivered_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		statusCode, boolToInt(succeeded), errMsg, id,
	)
	if err != nil {
		return fmt.Errorf("update webhook_delivery: %w", err)
	}
	return nil
}
// IncrementAttempt bumps the attempt counter and resets the timestamp for the next retry.
func IncrementAttempt(db *sql.DB, id string) error {
	_, err := db.Exec(`
		UPDATE webhook_deliveries
		SET attempt = attempt + 1, delivered_at = CURRENT_TIMESTAMP
		WHERE id = ?`, id,
	)
	if err != nil {
		return fmt.Errorf("increment attempt: %w", err)
	}
	return nil
}

// GetPendingDeliveries returns all undelivered records that are eligible for retry
// based on the backoff schedule. Called by the retry worker every 60 seconds.
func GetPendingDeliveries(db *sql.DB) ([]*WebhookDelivery, error) {
	rows, err := db.Query(`
		SELECT id, upload_id, project_id, url, status_code, attempt, succeeded, error, delivered_at
		FROM webhook_deliveries
		WHERE succeeded = 0 AND attempt < 5
		AND (
			(attempt = 1 AND delivered_at <= datetime('now', '-10 seconds'))  OR
			(attempt = 2 AND delivered_at <= datetime('now', '-30 seconds'))  OR
			(attempt = 3 AND delivered_at <= datetime('now', '-2 minutes'))   OR
			(attempt = 4 AND delivered_at <= datetime('now', '-10 minutes'))
		)`,
	)
	if err != nil {
		return nil, fmt.Errorf("query pending deliveries: %w", err)
	}
	defer rows.Close()
 
	var deliveries []*WebhookDelivery
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, d)
	}
	return deliveries, rows.Err()
}

func GetDeliveryByID(db *sql.DB, id string) (*WebhookDelivery, error) {
	row := db.QueryRow(`
		SELECT id, upload_id, project_id, url, status_code, attempt, succeeded, error, delivered_at
		FROM webhook_deliveries WHERE id = ?`, id,
	)
	return scanDeliveryRow(row)
}


func scanDelivery(rows *sql.Rows) (*WebhookDelivery, error) {
	var d WebhookDelivery
	var succeeded int
	err := rows.Scan(
		&d.ID, &d.UploadID, &d.ProjectID, &d.URL,
		&d.StatusCode, &d.Attempt, &d.Succeeded, &d.Error, &d.DeliveredAt,
	)
	if err != nil {
		return  nil, fmt.Errorf("scan delivery: %w", err)
	}
	d.Succeeded = succeeded == 1
	return &d, nil
}
func scanDeliveryRow(row *sql.Row) (*WebhookDelivery, error) {
	var d WebhookDelivery
	var succeeded int
	err := row.Scan(
		&d.ID, &d.UploadID, &d.ProjectID, &d.URL,
		&d.StatusCode, &d.Attempt, &succeeded, &d.Error, &d.DeliveredAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan delivery: %w", err)
	}
	d.Succeeded = succeeded == 1
	return  &d, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return  0
}