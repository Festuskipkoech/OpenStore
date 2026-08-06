package handlers_test

import (
	"errors"
	"net/http"
	"testing"
)

func TestHealth_Shallow(t *testing.T) {
	env := newTestEnv(t)
	w := doRequestNoAuth(t, env.router, http.MethodGet, "/health", nil)
	assertStatus(t, w, http.StatusOK)

	body := decodeBody(t, w)
	if body["status"] != "ok" {
		t.Errorf("expected status ok, got %v", body["status"])
	}
	if body["version"] == nil {
		t.Error("expected version in response")
	}
}

func TestHealth_Shallow_NoAuthRequired(t *testing.T) {
	env := newTestEnv(t)
	w := doRequestNoAuth(t, env.router, http.MethodGet, "/health", nil)
	assertStatus(t, w, http.StatusOK)
}

func TestHealth_Deep_BothUp(t *testing.T) {
	env := newTestEnv(t)
	w := doRequestNoAuth(t, env.router, http.MethodGet, "/health/deep", nil)
	assertStatus(t, w, http.StatusOK)

	body := decodeBody(t, w)
	if body["status"] != "ok" {
		t.Errorf("expected status ok, got %v", body["status"])
	}

	checks, ok := body["checks"].(map[string]any)
	if !ok {
		t.Fatal("expected checks object in response")
	}

	for _, name := range []string{"sqlite", "seaweedfs"} {
		check, ok := checks[name].(map[string]any)
		if !ok {
			t.Fatalf("expected %s check in response", name)
		}
		if check["status"] != "ok" {
			t.Errorf("expected %s status ok, got %v", name, check["status"])
		}
		if check["latency_ms"] == nil {
			t.Errorf("expected latency_ms in %s check", name)
		}
	}
}

func TestHealth_Deep_SeaweedDown(t *testing.T) {
	env := newTestEnv(t)
	env.seaweed.pingErr = errors.New("connection refused")

	w := doRequestNoAuth(t, env.router, http.MethodGet, "/health/deep", nil)
	assertStatus(t, w, http.StatusServiceUnavailable)

	body := decodeBody(t, w)
	if body["status"] != "degraded" {
		t.Errorf("expected status degraded, got %v", body["status"])
	}

	checks := body["checks"].(map[string]any)
	seaweed := checks["seaweedfs"].(map[string]any)
	if seaweed["status"] != "error" {
		t.Errorf("expected seaweedfs status error, got %v", seaweed["status"])
	}
	if seaweed["error"] == nil {
		t.Error("expected error field in seaweedfs check")
	}
}

func TestHealth_Deep_SQLiteDown(t *testing.T) {
	env := newTestEnv(t)
	env.db.Close()

	w := doRequestNoAuth(t, env.router, http.MethodGet, "/health/deep", nil)
	assertStatus(t, w, http.StatusServiceUnavailable)

	body := decodeBody(t, w)
	if body["status"] != "degraded" {
		t.Errorf("expected status degraded, got %v", body["status"])
	}

	checks := body["checks"].(map[string]any)
	sqlite := checks["sqlite"].(map[string]any)
	if sqlite["status"] != "error" {
		t.Errorf("expected sqlite status error, got %v", sqlite["status"])
	}
}

func TestHealth_Deep_NoAuthRequired(t *testing.T) {
	env := newTestEnv(t)
	w := doRequestNoAuth(t, env.router, http.MethodGet, "/health/deep", nil)
	assertStatus(t, w, http.StatusOK)
}