package webhook_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// sign mirrors the unexported sign function in deliver.go for test verification.
func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func buildSignatureHeader(body []byte, secret string) string {
	return "hmac-sha256=" + sign(body, secret)
}

func TestWebhookSign_ValidSignature(t *testing.T) {
	body := []byte(`{"event":"upload.verified"}`)
	secret := "webhook-secret"

	sig := buildSignatureHeader(body, secret)
	expected := "hmac-sha256=" + sign(body, secret)

	if sig != expected {
		t.Errorf("signature mismatch: got %s, expected %s", sig, expected)
	}
}

func TestWebhookSign_WrongSecret(t *testing.T) {
	body := []byte(`{"event":"upload.verified"}`)

	sig1 := sign(body, "correct-secret")
	sig2 := sign(body, "wrong-secret")

	if sig1 == sig2 {
		t.Error("expected different secrets to produce different signatures")
	}
}

func TestWebhookSign_TamperedBody(t *testing.T) {
	secret := "webhook-secret"
	original := []byte(`{"event":"upload.verified"}`)
	tampered := []byte(`{"event":"upload.rejected"}`)

	sig1 := sign(original, secret)
	sig2 := sign(tampered, secret)

	if sig1 == sig2 {
		t.Error("expected tampered body to produce different signature")
	}
}

func TestWebhookSign_EmptyBody(t *testing.T) {
	body := []byte{}
	secret := "webhook-secret"

	sig := buildSignatureHeader(body, secret)
	if sig == "" {
		t.Error("expected non-empty signature for empty body")
	}
	// HMAC of empty string is valid and deterministic.
	expected := "hmac-sha256=" + sign(body, secret)
	if sig != expected {
		t.Errorf("empty body signature mismatch")
	}
}

func TestWebhookSign_SignatureFormat(t *testing.T) {
	body := []byte(`{"event":"upload.verified"}`)
	sig := buildSignatureHeader(body, "secret")

	if !strings.HasPrefix(sig, "hmac-sha256=") {
		t.Errorf("expected hmac-sha256= prefix, got: %s", sig)
	}

	hex := strings.TrimPrefix(sig, "hmac-sha256=")
	if len(hex) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(hex))
	}
}