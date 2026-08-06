package models

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)
 
var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("conflict")
 
type Project struct {
	ID string `json:"project_id"`
	Name string `json:"name"`
	APIKeyHash string `json:"-"`
	WebhookURL string `json:"webhook_url"`
	WebhookSecret string `json:"-"`
	AllowedOrigins []string `json:"allowed_origins"`
	QuotaBytes int64 `json:"quota_bytes"`
	UsedBytes int64 `json:"used_bytes"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateProjectParams struct {
	ID string
	Name  string
	APIKeyHash string
	WebhookURL string
	WebhookSecret string
	AllowedOrigins []string
	QuotaBytes int64
}
 
type UpdateProjectParams struct {
	Name *string
	WebhookURL *string
	WebhookSecret *string
	AllowedOrigins []string
	QuotaBytes *int64
}

func CreateProject(db *sql.DB, p CreateProjectParams) (*Project, error) {
	origins, err := json.Marshal(p.AllowedOrigins)
	if err != nil {
		return nil, fmt.Errorf("marshal allowed_origins: %w", err)
	}
 
	_, err = db.Exec(`
		INSERT INTO projects (id, name, api_key_hash, webhook_url, webhook_secret, allowed_origins, quota_bytes)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.APIKeyHash, p.WebhookURL, p.WebhookSecret, string(origins), p.QuotaBytes,
	)
	if err != nil {
		return nil, fmt.Errorf("insert project: %w", err)
	}
 
	return GetProject(db, p.ID)
}
 
func CreateProjectTx(tx *sql.Tx, p CreateProjectParams) (*Project, error) {
	origins, err := json.Marshal(p.AllowedOrigins)
	if err != nil {
		return nil, fmt.Errorf("marshal allowed_origins: %w", err)
	}
 
	_, err = tx.Exec(`
		INSERT INTO projects (id, name, api_key_hash, webhook_url, webhook_secret, allowed_origins, quota_bytes)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.APIKeyHash, p.WebhookURL, p.WebhookSecret, string(origins), p.QuotaBytes,
	)
	if err != nil {
		return nil, fmt.Errorf("insert project: %w", err)
	}
 
	return getProjectTx(tx, p.ID)
}

func GetProject(db *sql.DB, id string) (*Project, error) {
	row := db.QueryRow(`
		SELECT id, name, api_key_hash, webhook_url, webhook_secret,
		       allowed_origins, quota_bytes, used_bytes, created_at, updated_at
		FROM projects WHERE id = ?`, id)
 
	return scanProject(row)
}

func getProjectTx(tx *sql.Tx, id string) (*Project, error) {
	row := tx.QueryRow(`
		SELECT id, name, api_key_hash, webhook_url, webhook_secret,
		       allowed_origins, quota_bytes, used_bytes, created_at, updated_at
		FROM projects WHERE id = ?`, id)
 
	return scanProject(row)
}

func UpdateProject(db *sql.DB, id string, p UpdateProjectParams) (*Project, error) {
	setClauses := []string{"updated_at = CURRENT_TIMESTAMP"}
	args := []any{}
 
	if p.Name != nil {
		setClauses = append(setClauses, "name = ?")
		args = append(args, *p.Name)
	}
	if p.WebhookURL != nil {
		setClauses = append(setClauses, "webhook_url = ?")
		args = append(args, *p.WebhookURL)
	}
	if p.WebhookSecret != nil {
		setClauses = append(setClauses, "webhook_secret = ?")
		args = append(args, *p.WebhookSecret)
	}
	if p.AllowedOrigins != nil {
		origins, err := json.Marshal(p.AllowedOrigins)
		if err != nil {
			return nil, fmt.Errorf("marshal allowed_origins: %w", err)
		}
		setClauses = append(setClauses, "allowed_origins = ?")
		args = append(args, string(origins))
	}
	if p.QuotaBytes != nil {
		setClauses = append(setClauses, "quota_bytes = ?")
		args = append(args, *p.QuotaBytes)
	}
 
	args = append(args, id)
 
	_, err := db.Exec(
		"UPDATE projects SET "+strings.Join(setClauses, ", ")+" WHERE id = ?",
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}
 
	return GetProject(db, id)
}
func UpdateProjectTx(tx *sql.Tx, id string, p UpdateProjectParams) (*Project, error) {
	setClauses := []string{"updated_at = CURRENT_TIMESTAMP"}
	args := []any{}
 
	if p.Name != nil {
		setClauses = append(setClauses, "name = ?")
		args = append(args, *p.Name)
	}
	if p.WebhookURL != nil {
		setClauses = append(setClauses, "webhook_url = ?")
		args = append(args, *p.WebhookURL)
	}
	if p.WebhookSecret != nil {
		setClauses = append(setClauses, "webhook_secret = ?")
		args = append(args, *p.WebhookSecret)
	}
	if p.AllowedOrigins != nil {
		origins, err := json.Marshal(p.AllowedOrigins)
		if err != nil {
			return nil, fmt.Errorf("marshal allowed_origins: %w", err)
		}
		setClauses = append(setClauses, "allowed_origins = ?")
		args = append(args, string(origins))
	}
	if p.QuotaBytes != nil {
		setClauses = append(setClauses, "quota_bytes = ?")
		args = append(args, *p.QuotaBytes)
	}
 
	args = append(args, id)
 
	_, err := tx.Exec(
		"UPDATE projects SET "+strings.Join(setClauses, ", ")+" WHERE id = ?",
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}
 
	return getProjectTx(tx, id)
}

func DeleteProject(db *sql.DB, id string) error {
	res, err := db.Exec("DELETE FROM projects WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
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

func scanProject(row *sql.Row) (*Project, error) {
	var p Project
	var originsJSON string
	
	err := row.Scan(
		&p.ID, &p.Name, &p.APIKeyHash, &p.WebhookURL, &p.WebhookSecret,
		&originsJSON, &p.QuotaBytes, &p.UsedBytes, &p.CreatedAt, &p.UpdatedAt,
	)
	
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan project: %w", err)
	}
 
	if err := json.Unmarshal([]byte(originsJSON), &p.AllowedOrigins); err != nil {
		return nil, fmt.Errorf("unmarshal allowed_origins: %w", err)
	}

	return  &p, nil
}