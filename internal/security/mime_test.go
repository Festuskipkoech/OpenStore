package security_test

import (
	"testing"

	"openstore/internal/security"
)

func TestIsAllowedMIME_AllowedType(t *testing.T) {
	if !security.IsAllowedMIME("image/jpeg", []string{"image/jpeg", "image/png"}) {
		t.Error("expected image/jpeg to be allowed")
	}
}

func TestIsAllowedMIME_DisallowedType(t *testing.T) {
	if security.IsAllowedMIME("image/gif", []string{"image/jpeg", "image/png"}) {
		t.Error("expected image/gif to be rejected")
	}
}

func TestIsAllowedMIME_EmptyAllowlist(t *testing.T) {
	if security.IsAllowedMIME("image/jpeg", []string{}) {
		t.Error("expected any type to be rejected against empty allowlist")
	}
}

func TestIsAllowedMIME_CaseSensitive(t *testing.T) {
	// MIME types are case-sensitive — Image/JPEG must not match image/jpeg.
	if security.IsAllowedMIME("Image/JPEG", []string{"image/jpeg"}) {
		t.Error("expected case mismatch to be rejected")
	}
}