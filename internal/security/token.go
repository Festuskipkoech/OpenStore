package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrTokenInvalid = errors.New("token signature is invalid")
	ErrTokenExpired = errors.New("token has expired")
	ErrTokenMalformed = errors.New("token is malformed")
)

// Tokenizer signs and verifies upload and read tokens.
// The API key is the signing secret and never leaves the server.
type Tokenizer struct {
	apiKey []byte
}

func NewTokenizer(apiKey string) *Tokenizer {
	return &Tokenizer{apiKey: []byte(apiKey)}
}

// TokenClaims holds the values embedded in a signed upload token.
type TokenClaims struct {
	UploadID string
	BucketName string
	MIMEType string
	FileSize int64
	ExpiresAt time.Time
}

// ReadTokenClaims holds the values embedded in a signed read token.
type ReadTokenClaims struct {
	UploadID string
	ExpiresAt time.Time
}

// Sign produces an HMAC-SHA256 signed upload token encoding the given claims.
// Format: upload_id:bucket_name:mime_type:file_size:expires_unix:hex_signature
func (t *Tokenizer) Sign(c TokenClaims) string {
	payload := buildPayload(c.UploadID, c.BucketName, c.MIMEType, c.FileSize, c.ExpiresAt.Unix())
	sig := t.sign(payload)
	return payload + ":" + sig
}

// Verify parses an upload token, checks the signature, checks expiry, and returns
// the embedded claims. Returns a typed error on any failure.
func (t *Tokenizer) Verify(token string) (TokenClaims, error) {
	parts := strings.Split(token, ":")
	if len(parts) != 6 {
		return TokenClaims{}, ErrTokenMalformed
	}

	payload := strings.Join(parts[:5], ":")
	providedSig := parts[5]
	expectedSig := t.sign(payload)

	if !hmac.Equal([]byte(providedSig), []byte(expectedSig)) {
		return TokenClaims{}, ErrTokenInvalid
	}

	fileSize, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return TokenClaims{}, ErrTokenMalformed
	}

	expiresUnix, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		return TokenClaims{}, ErrTokenMalformed
	}

	expiresAt := time.Unix(expiresUnix, 0)
	if time.Now().After(expiresAt) {
		return TokenClaims{}, ErrTokenExpired
	}

	return TokenClaims{
		UploadID: parts[0],
		BucketName: parts[1],
		MIMEType: parts[2],
		FileSize: fileSize,
		ExpiresAt: expiresAt,
	}, nil
}

// SignRead produces an HMAC-SHA256 signed read token for a private file.
// Format: read:upload_id:expires_unix:hex_signature
// The "read:" prefix prevents upload tokens being used as read tokens and vice versa.
func (t *Tokenizer) SignRead(c ReadTokenClaims) string {
	payload := buildReadPayload(c.UploadID, c.ExpiresAt.Unix())
	sig := t.sign(payload)
	return payload + ":" + sig
}

// VerifyRead parses a read token, checks the signature, checks expiry, and returns
// the embedded claims. Returns a typed error on any failure.
func (t *Tokenizer) VerifyRead(token string) (ReadTokenClaims, error) {
	parts := strings.Split(token, ":")
	if len(parts) != 4 {
		return ReadTokenClaims{}, ErrTokenMalformed
	}

	// Enforce the read prefix — rejects upload tokens presented as read tokens.
	if parts[0] != "read" {
		return ReadTokenClaims{}, ErrTokenMalformed
	}

	payload := strings.Join(parts[:3], ":")
	providedSig := parts[3]
	expectedSig := t.sign(payload)

	if !hmac.Equal([]byte(providedSig), []byte(expectedSig)) {
		return ReadTokenClaims{}, ErrTokenInvalid
	}

	expiresUnix, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return ReadTokenClaims{}, ErrTokenMalformed
	}

	expiresAt := time.Unix(expiresUnix, 0)
	if time.Now().After(expiresAt) {
		return ReadTokenClaims{}, ErrTokenExpired
	}

	return ReadTokenClaims{
		UploadID:  parts[1],
		ExpiresAt: expiresAt,
	}, nil
}

func (t *Tokenizer) sign(payload string) string {
	mac := hmac.New(sha256.New, t.apiKey)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func buildPayload(uploadID, bucketName, mimeType string, fileSize, expiresUnix int64) string {
	return fmt.Sprintf("%s:%s:%s:%d:%d", uploadID, bucketName, mimeType, fileSize, expiresUnix)
}

func buildReadPayload(uploadID string, expiresUnix int64) string {
	return fmt.Sprintf("read:%s:%d", uploadID, expiresUnix)
}