package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"time"
)

const version = "1.0.0"

type Pinger interface {
	Ping(ctx context.Context) error
}

type checkResult struct {
	Status string `json:"status"`
	LatencyMs int64  `json:"latency_ms"`
	Error string `json:"error,omitempty"`
}

type HealthHandler struct {
	db *sql.DB
	seaweed Pinger
}

func NewHealthHandler(db *sql.DB, seaweed Pinger) *HealthHandler {
	return &HealthHandler{db: db, seaweed: seaweed}
}

func (h *HealthHandler) Shallow(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": version,
	})
}

func (h *HealthHandler) Deep(w http.ResponseWriter, r *http.Request) {
	type response struct {
		Status string `json:"status"`
		Checks map[string]checkResult `json:"checks"`
	}

	checks := make(map[string]checkResult)
	allOK := true

	sqliteResult := pingDB(h.db)
	checks["sqlite"] = sqliteResult
	if sqliteResult.Status != "ok" {
		allOK = false
	}

	seaweedResult := pingSeaweedFS(r.Context(), h.seaweed)
	checks["seaweedfs"] = seaweedResult
	if seaweedResult.Status != "ok" {
		allOK = false
	}

	status := "ok"
	httpStatus := http.StatusOK
	if !allOK {
		status = "degraded"
		httpStatus = http.StatusServiceUnavailable
	}

	writeJSON(w, httpStatus, response{
		Status: status,
		Checks: checks,
	})
}

func pingDB(db *sql.DB) checkResult {
	start := time.Now()
	err := db.Ping()
	ms := time.Since(start).Milliseconds()
	if err != nil {
		return checkResult{Status: "error", LatencyMs: ms, Error: err.Error()}
	}
	return checkResult{Status: "ok", LatencyMs: ms}
}

func pingSeaweedFS(ctx context.Context, client Pinger) checkResult {
	start := time.Now()
	err := client.Ping(ctx)
	ms := time.Since(start).Milliseconds()
	if err != nil {
		return checkResult{Status: "error", LatencyMs: ms, Error: err.Error()}
	}
	return checkResult{Status: "ok", LatencyMs: ms}
}
