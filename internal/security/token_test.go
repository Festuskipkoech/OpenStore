package security_test

import (
	"testing"
	"time"

	"openstore/internal/security"
)

func newTokenizer() *security.Tokenizer {
	return security.NewTokenizer("test-api-key")
}

func validUploadClaims() security.TokenClaims {
	return security.TokenClaims{
		UploadID:   "01J4M3PQXZ",
		BucketName: "mediavault-avatars",
		MIMEType:   "image/jpeg",
		FileSize:   204800,
		ExpiresAt:  time.Now().UTC().Add(5 * time.Minute),
	}
}

func validReadClaims(uploadID string) security.ReadTokenClaims {
	return security.ReadTokenClaims{
		UploadID:  uploadID,
		ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
	}
}

// Upload token tests

func TestToken_SignAndVerify_Valid(t *testing.T) {
	tz := newTokenizer()
	claims := validUploadClaims()

	token := tz.Sign(claims)
	got, err := tz.Verify(token)
	if err != nil {
		t.Fatalf("expected valid token to pass, got: %v", err)
	}

	if got.UploadID != claims.UploadID {
		t.Errorf("upload_id mismatch: got %s, expected %s", got.UploadID, claims.UploadID)
	}
	if got.BucketName != claims.BucketName {
		t.Errorf("bucket_name mismatch: got %s, expected %s", got.BucketName, claims.BucketName)
	}
	if got.MIMEType != claims.MIMEType {
		t.Errorf("mime_type mismatch: got %s, expected %s", got.MIMEType, claims.MIMEType)
	}
	if got.FileSize != claims.FileSize {
		t.Errorf("file_size mismatch: got %d, expected %d", got.FileSize, claims.FileSize)
	}
}

func TestToken_TamperedPayload(t *testing.T) {
	tz := newTokenizer()
	token := tz.Sign(validUploadClaims())

	// Flip one character in the payload portion.
	tampered := "X" + token[1:]
	if _, err := tz.Verify(tampered); err == nil {
		t.Error("expected tampered token to fail, got nil")
	}
}

func TestToken_TamperedSignature(t *testing.T) {
	tz := newTokenizer()
	token := tz.Sign(validUploadClaims())

	// Corrupt the last 4 characters — the signature portion.
	tampered := token[:len(token)-4] + "0000"
	if _, err := tz.Verify(tampered); err == nil {
		t.Error("expected tampered signature to fail, got nil")
	}
}

func TestToken_Expired(t *testing.T) {
	tz := newTokenizer()
	claims := validUploadClaims()
	claims.ExpiresAt = time.Now().UTC().Add(-1 * time.Minute)

	token := tz.Sign(claims)
	_, err := tz.Verify(token)
	if err != security.ErrTokenExpired {
		t.Errorf("expected ErrTokenExpired, got: %v", err)
	}
}

func TestToken_Malformed_TooFewParts(t *testing.T) {
	tz := newTokenizer()
	_, err := tz.Verify("only:four:parts:here")
	if err != security.ErrTokenMalformed {
		t.Errorf("expected ErrTokenMalformed, got: %v", err)
	}
}

func TestToken_WrongSigningKey(t *testing.T) {
	signer := security.NewTokenizer("key-a")
	verifier := security.NewTokenizer("key-b")

	token := signer.Sign(validUploadClaims())
	if _, err := verifier.Verify(token); err == nil {
		t.Error("expected token signed with different key to fail")
	}
}

// Read token tests

func TestReadToken_SignAndVerify_Valid(t *testing.T) {
	tz := newTokenizer()
	claims := validReadClaims("01J4M3PQXZ")

	token := tz.SignRead(claims)
	got, err := tz.VerifyRead(token)
	if err != nil {
		t.Fatalf("expected valid read token to pass, got: %v", err)
	}

	if got.UploadID != claims.UploadID {
		t.Errorf("upload_id mismatch: got %s, expected %s", got.UploadID, claims.UploadID)
	}
}

func TestReadToken_Expired(t *testing.T) {
	tz := newTokenizer()
	claims := validReadClaims("01J4M3PQXZ")
	claims.ExpiresAt = time.Now().UTC().Add(-1 * time.Minute)

	token := tz.SignRead(claims)
	_, err := tz.VerifyRead(token)
	if err != security.ErrTokenExpired {
		t.Errorf("expected ErrTokenExpired, got: %v", err)
	}
}

func TestReadToken_UploadTokenRejectedAsReadToken(t *testing.T) {
	tz := newTokenizer()
	// An upload token must not be accepted by VerifyRead — different prefix.
	uploadToken := tz.Sign(validUploadClaims())
	if _, err := tz.VerifyRead(uploadToken); err == nil {
		t.Error("expected upload token to be rejected by VerifyRead")
	}
}

func TestReadToken_TamperedSignature(t *testing.T) {
	tz := newTokenizer()
	token := tz.SignRead(validReadClaims("01J4M3PQXZ"))

	tampered := token[:len(token)-4] + "0000"
	if _, err := tz.VerifyRead(tampered); err == nil {
		t.Error("expected tampered read token to fail")
	}
}

func TestReadToken_Malformed(t *testing.T) {
	tz := newTokenizer()
	_, err := tz.VerifyRead("read:only:two")
	if err != security.ErrTokenMalformed {
		t.Errorf("expected ErrTokenMalformed, got: %v", err)
	}
}