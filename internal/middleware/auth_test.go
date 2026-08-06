package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"openstore/internal/middleware"
)

func applyAuth(key string, next http.Handler) http.Handler {
	return middleware.Auth(key)(next)
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func doAuthRequest(t *testing.T, handler http.Handler, authHeader string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w.Code
}

func TestAuth_MatchingKey(t *testing.T) {
	handler := applyAuth("secret-key", okHandler())
	if code := doAuthRequest(t, handler, "Bearer secret-key"); code != http.StatusOK {
		t.Errorf("expected 200, got %d", code)
	}
}

func TestAuth_DifferentKeySameLength(t *testing.T) {
	handler := applyAuth("aaaaaaaa", okHandler())
	if code := doAuthRequest(t, handler, "Bearer bbbbbbbb"); code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", code)
	}
}

func TestAuth_DifferentKeyDifferentLength(t *testing.T) {
	handler := applyAuth("short", okHandler())
	if code := doAuthRequest(t, handler, "Bearer muchlongerkey"); code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", code)
	}
}

func TestAuth_EmptyIncomingKey(t *testing.T) {
	handler := applyAuth("secret-key", okHandler())
	if code := doAuthRequest(t, handler, "Bearer "); code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", code)
	}
}

func TestAuth_NoAuthorizationHeader(t *testing.T) {
	handler := applyAuth("secret-key", okHandler())
	if code := doAuthRequest(t, handler, ""); code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", code)
	}
}

func TestAuth_MissingBearerPrefix(t *testing.T) {
	handler := applyAuth("secret-key", okHandler())
	if code := doAuthRequest(t, handler, "secret-key"); code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", code)
	}
}