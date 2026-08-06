package config_test

import (
	"testing"

	"openstore/internal/config"
)

func TestResolveTTL_PerRequestWins(t *testing.T) {
	got := config.ResolveTTL(600, 300, 300, 86400)
	if got != 600 {
		t.Errorf("expected 600, got %d", got)
	}
}

func TestResolveTTL_BucketWinsWhenNoPerRequest(t *testing.T) {
	got := config.ResolveTTL(0, 1800, 300, 86400)
	if got != 1800 {
		t.Errorf("expected 1800, got %d", got)
	}
}

func TestResolveTTL_GlobalDefaultFallback(t *testing.T) {
	got := config.ResolveTTL(0, 0, 300, 86400)
	if got != 300 {
		t.Errorf("expected 300, got %d", got)
	}
}

func TestResolveTTL_PerRequestCappedAtMax(t *testing.T) {
	got := config.ResolveTTL(90000, 300, 300, 86400)
	if got != 86400 {
		t.Errorf("expected 86400, got %d", got)
	}
}

func TestResolveTTL_BucketTTLCappedAtMax(t *testing.T) {
	got := config.ResolveTTL(0, 90000, 300, 86400)
	if got != 86400 {
		t.Errorf("expected 86400, got %d", got)
	}
}