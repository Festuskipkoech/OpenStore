package handlers_test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"openstore/internal/security"

	"github.com/chai2010/webp"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// validJPEG returns 12+ bytes with a valid JPEG magic header followed by padding.
func validJPEGBytes(_ int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.White)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		panic("failed to encode test JPEG: " + err.Error())
	}
	return buf.Bytes()
}

// pngBytesAsJPEG returns PNG magic bytes — used to trigger magic byte mismatch.
func pngBytesAsJPEG(size int) []byte {
	data := make([]byte, size)
	copy(data, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x00})
	return data
}
// realPNGBytes generates a real 1x1 white pixel PNG using Go stdlib.
func realPNGBytes() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.White)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic("failed to encode test PNG: " + err.Error())
	}
	return buf.Bytes()
}

// realGIFBytes generates a real 1x1 white pixel GIF using Go stdlib.
func realGIFBytes() []byte {
	img := image.NewPaletted(image.Rect(0, 0, 1, 1), []color.Color{color.White})
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &gif.GIF{
		Image: []*image.Paletted{img},
		Delay: []int{0},
	}); err != nil {
		panic("failed to encode test GIF: " + err.Error())
	}
	return buf.Bytes()
}

// realWebPBytes generates a minimal lossy WebP using chai2010/webp.
// The hardcoded VP8L (lossless) sequence previously used here fails to decode
// under golang.org/x/image/webp, which only supports lossy (VP8) and extended (VP8X).
func realWebPBytes() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.White)
	var buf bytes.Buffer
	if err := webp.Encode(&buf, img, &webp.Options{Lossless: false, Quality: 80}); err != nil {
		panic("failed to encode test WebP: " + err.Error())
	}
	return buf.Bytes()
}

// realPDFBytes generates a minimal single-page PDF using pdfcpu's own engine.
// A 1x1 white PNG is imported as a page — pdfcpu generates it, pdfcpu validates it.
// This guarantees the fixture never drifts out of spec across pdfcpu version upgrades.
func realPDFBytes() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.White)

	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		panic("failed to encode PNG for PDF fixture: " + err.Error())
	}

	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed

	var pdfBuf bytes.Buffer
	if err := api.ImportImages(nil, &pdfBuf, []io.Reader{&pngBuf}, nil, conf); err != nil {
		panic("failed to generate PDF fixture via pdfcpu: " + err.Error())
	}

	return pdfBuf.Bytes()
}

// magicBytesOnly builds a 1024-byte buffer with the correct magic bytes for types
// where deep inspection is skipped (video and audio). Only the header matters.
func magicBytesOnly(header []byte) []byte {
	buf := make([]byte, 1024)
	copy(buf, header)
	return buf
}

func TestStream_ValidPNG_PublicBucket(t *testing.T) {
	env := newFullTestEnv(t)
	_, _, token := insertPendingUploadForStreamWithMIME(t, env, "public", "images", "image/png")
	uploadID := "stream-mime-" + t.Name()

	path := fmt.Sprintf("/upload/%s?token=%s", uploadID, token)
	w := doRawRequest(t, env.router, http.MethodPut, path, "image/png", bytes.NewReader(realPNGBytes()), map[string]string{
		"Content-Length": fmt.Sprintf("%d", len(realPNGBytes())),
	})
	assertStatus(t, w, http.StatusOK)
	body := decodeBody(t, w)
	if body["status"] != "verified" {
		t.Errorf("expected status verified, got %v", body["status"])
	}
	if body["public_url"] == nil {
		t.Error("expected public_url for public bucket")
	}
}

func TestStream_ValidGIF_PublicBucket(t *testing.T) {
	env := newFullTestEnv(t)
	_, _, token := insertPendingUploadForStreamWithMIME(t, env, "public", "images", "image/gif")
	uploadID := "stream-mime-" + t.Name()

	path := fmt.Sprintf("/upload/%s?token=%s", uploadID, token)
	w := doRawRequest(t, env.router, http.MethodPut, path, "image/gif", bytes.NewReader(realGIFBytes()), map[string]string{
		"Content-Length": fmt.Sprintf("%d", len(realGIFBytes())),
	})
	assertStatus(t, w, http.StatusOK)
	body := decodeBody(t, w)
	if body["status"] != "verified" {
		t.Errorf("expected status verified, got %v", body["status"])
	}
}

func TestStream_ValidWebP_PublicBucket(t *testing.T) {
	env := newFullTestEnv(t)
	_, _, token := insertPendingUploadForStreamWithMIME(t, env, "public", "images", "image/webp")
	uploadID := "stream-mime-" + t.Name()

	path := fmt.Sprintf("/upload/%s?token=%s", uploadID, token)
	w := doRawRequest(t, env.router, http.MethodPut, path, "image/webp", bytes.NewReader(realWebPBytes()), map[string]string{
		"Content-Length": fmt.Sprintf("%d", len(realWebPBytes())),
	})
	assertStatus(t, w, http.StatusOK)
	body := decodeBody(t, w)
	if body["status"] != "verified" {
		t.Errorf("expected status verified, got %v", body["status"])
	}
}

func TestStream_ValidPDF_PrivateBucket(t *testing.T) {
	env := newFullTestEnv(t)
	_, _, token := insertPendingUploadForStreamWithMIME(t, env, "private", "documents", "application/pdf")
	uploadID := "stream-mime-" + t.Name()

	path := fmt.Sprintf("/upload/%s?token=%s", uploadID, token)
	w := doRawRequest(t, env.router, http.MethodPut, path, "application/pdf", bytes.NewReader(realPDFBytes()), map[string]string{
		"Content-Length": fmt.Sprintf("%d", len(realPDFBytes())),
	})
	assertStatus(t, w, http.StatusOK)
	body := decodeBody(t, w)
	if body["status"] != "verified" {
		t.Errorf("expected status verified, got %v", body["status"])
	}
	if body["public_url"] != nil {
		t.Error("expected public_url to be null for private bucket")
	}
}

func TestStream_ValidMP4_PrivateBucket(t *testing.T) {
	env := newFullTestEnv(t)
	_, _, token := insertPendingUploadForStreamWithMIME(t, env, "private", "videos", "video/mp4")
	uploadID := "stream-mime-" + t.Name()

	// MP4 ftyp box at offset 4
	mp4Bytes := magicBytesOnly([]byte{0x00, 0x00, 0x00, 0x00, 0x66, 0x74, 0x79, 0x70, 0x00, 0x00, 0x00, 0x00})
	path := fmt.Sprintf("/upload/%s?token=%s", uploadID, token)
	w := doRawRequest(t, env.router, http.MethodPut, path, "video/mp4", bytes.NewReader(mp4Bytes), map[string]string{
		"Content-Length": fmt.Sprintf("%d", len(mp4Bytes)),
	})
	assertStatus(t, w, http.StatusOK)
	body := decodeBody(t, w)
	if body["status"] != "verified" {
		t.Errorf("expected status verified, got %v", body["status"])
	}
}

func TestStream_ValidWebM_PrivateBucket(t *testing.T) {
	env := newFullTestEnv(t)
	_, _, token := insertPendingUploadForStreamWithMIME(t, env, "private", "videos", "video/webm")
	uploadID := "stream-mime-" + t.Name()

	webmBytes := magicBytesOnly([]byte{0x1A, 0x45, 0xDF, 0xA3})
	path := fmt.Sprintf("/upload/%s?token=%s", uploadID, token)
	w := doRawRequest(t, env.router, http.MethodPut, path, "video/webm", bytes.NewReader(webmBytes), map[string]string{
		"Content-Length": fmt.Sprintf("%d", len(webmBytes)),
	})
	assertStatus(t, w, http.StatusOK)
	body := decodeBody(t, w)
	if body["status"] != "verified" {
		t.Errorf("expected status verified, got %v", body["status"])
	}
}

func TestStream_ValidMP3_PrivateBucket(t *testing.T) {
	env := newFullTestEnv(t)
	_, _, token := insertPendingUploadForStreamWithMIME(t, env, "private", "audio", "audio/mpeg")
	uploadID := "stream-mime-" + t.Name()

	mp3Bytes := magicBytesOnly([]byte{0x49, 0x44, 0x33})
	path := fmt.Sprintf("/upload/%s?token=%s", uploadID, token)
	w := doRawRequest(t, env.router, http.MethodPut, path, "audio/mpeg", bytes.NewReader(mp3Bytes), map[string]string{
		"Content-Length": fmt.Sprintf("%d", len(mp3Bytes)),
	})
	assertStatus(t, w, http.StatusOK)
	body := decodeBody(t, w)
	if body["status"] != "verified" {
		t.Errorf("expected status verified, got %v", body["status"])
	}
}

func TestStream_ValidWAV_PrivateBucket(t *testing.T) {
	env := newFullTestEnv(t)
	_, _, token := insertPendingUploadForStreamWithMIME(t, env, "private", "audio", "audio/wav")
	uploadID := "stream-mime-" + t.Name()

	wavBytes := magicBytesOnly([]byte{0x52, 0x49, 0x46, 0x46})
	path := fmt.Sprintf("/upload/%s?token=%s", uploadID, token)
	w := doRawRequest(t, env.router, http.MethodPut, path, "audio/wav", bytes.NewReader(wavBytes), map[string]string{
		"Content-Length": fmt.Sprintf("%d", len(wavBytes)),
	})
	assertStatus(t, w, http.StatusOK)
	body := decodeBody(t, w)
	if body["status"] != "verified" {
		t.Errorf("expected status verified, got %v", body["status"])
	}
}

func TestStream_ValidOGG_PrivateBucket(t *testing.T) {
	env := newFullTestEnv(t)
	_, _, token := insertPendingUploadForStreamWithMIME(t, env, "private", "audio", "audio/ogg")
	uploadID := "stream-mime-" + t.Name()

	oggBytes := magicBytesOnly([]byte{0x4F, 0x67, 0x67, 0x53})
	path := fmt.Sprintf("/upload/%s?token=%s", uploadID, token)
	w := doRawRequest(t, env.router, http.MethodPut, path, "audio/ogg", bytes.NewReader(oggBytes), map[string]string{
		"Content-Length": fmt.Sprintf("%d", len(oggBytes)),
	})
	assertStatus(t, w, http.StatusOK)
	body := decodeBody(t, w)
	if body["status"] != "verified" {
		t.Errorf("expected status verified, got %v", body["status"])
	}
}

func TestStream_ValidFLAC_PrivateBucket(t *testing.T) {
	env := newFullTestEnv(t)
	_, _, token := insertPendingUploadForStreamWithMIME(t, env, "private", "audio", "audio/flac")
	uploadID := "stream-mime-" + t.Name()

	flacBytes := magicBytesOnly([]byte{0x66, 0x4C, 0x61, 0x43})
	path := fmt.Sprintf("/upload/%s?token=%s", uploadID, token)
	w := doRawRequest(t, env.router, http.MethodPut, path, "audio/flac", bytes.NewReader(flacBytes), map[string]string{
		"Content-Length": fmt.Sprintf("%d", len(flacBytes)),
	})
	assertStatus(t, w, http.StatusOK)
	body := decodeBody(t, w)
	if body["status"] != "verified" {
		t.Errorf("expected status verified, got %v", body["status"])
	}
}

func insertPendingUploadForStream(t *testing.T, env *testEnv, access string) (uploadID, objectKey, token string) {
	t.Helper()
	projectID, bucketID := insertTestProject(t, env.db, access)

	uploadID = fmt.Sprintf("stream-%s-%d", t.Name(), time.Now().UnixNano())
	objectKey = "images/" + projectID + "/2026/08/" + uploadID + ".jpg"

	_, err := env.db.Exec(`
		INSERT INTO uploads (id, project_id, bucket_id, object_key, original_name, content_type,
		size_bytes, status, expires_at)
		VALUES (?, ?, ?, ?, 'test.jpg', 'image/jpeg', 1024, 'pending', datetime('now', '+5 minutes'))`,
		uploadID, projectID, bucketID, objectKey,
	)
	if err != nil {
		t.Fatalf("insert pending upload: %v", err)
	}

	token = signUploadToken(t, env.tokenizer, uploadID, "test-bucket", "image/jpeg", 1024, 5*time.Minute)
	return uploadID, objectKey, token
}

func doPUT(t *testing.T, env *testEnv, uploadID, token string, body []byte, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	path := fmt.Sprintf("/upload/%s?token=%s", uploadID, token)
	return doRawRequest(t, env.router, http.MethodPut, path, contentType, bytes.NewReader(body), map[string]string{
		"Content-Length": fmt.Sprintf("%d", len(body)),
	})
}

func TestStream_MissingToken(t *testing.T) {
	env := newFullTestEnv(t)
	uploadID, _, _ := insertPendingUploadForStream(t, env, "public")

	path := "/upload/" + uploadID
	w := doRawRequest(t, env.router, http.MethodPut, path, "image/jpeg", bytes.NewReader(validJPEGBytes(1024)), nil)
	assertStatus(t, w, http.StatusUnauthorized)
	assertErrorCode(t, w, "unauthorized")
}

func TestStream_ExpiredToken(t *testing.T) {
	env := newFullTestEnv(t)
	uploadID, _, _ := insertPendingUploadForStream(t, env, "public")

	expiredToken := env.tokenizer.Sign(security.TokenClaims{
		UploadID: uploadID,
		BucketName: "test-bucket",
		MIMEType: "image/jpeg",
		FileSize: 1024,
		ExpiresAt: time.Now().UTC().Add(-1 * time.Minute),
	})

	w := doPUT(t, env, uploadID, expiredToken, validJPEGBytes(1024), "image/jpeg")
	assertStatus(t, w, http.StatusUnauthorized)
	assertErrorCode(t, w, "unauthorized")
}

func TestStream_ContentTypeMismatch(t *testing.T) {
	env := newFullTestEnv(t)
	uploadID, _, token := insertPendingUploadForStream(t, env, "public")

	// token says image/jpeg but we send image/png
	path := fmt.Sprintf("/upload/%s?token=%s", uploadID, token)
	w := doRawRequest(t, env.router, http.MethodPut, path, "image/png", bytes.NewReader(validJPEGBytes(1024)), nil)
	assertStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "invalid_request")
}

func TestStream_MagicBytesMismatch(t *testing.T) {
	env := newFullTestEnv(t)
	uploadID, objectKey, token := insertPendingUploadForStream(t, env, "public")

	// PNG bytes declared as JPEG — magic bytes check must fail
	w := doPUT(t, env, uploadID, token, pngBytesAsJPEG(1024), "image/jpeg")
	assertStatus(t, w, http.StatusUnprocessableEntity)
	assertErrorCode(t, w, "verification_failed")

	// object must have been deleted from SeaweedFS
	if env.seaweed.deleteCallCount() == 0 {
		t.Error("expected DeleteObject to be called after magic byte failure")
	}

	// upload must be marked rejected
	var status string
	env.db.QueryRow("SELECT status FROM uploads WHERE id = ?", uploadID).Scan(&status)
	if status != "rejected" {
		t.Errorf("expected status rejected, got %s", status)
	}

	_ = objectKey
}

func TestStream_FileTooSmall(t *testing.T) {
	env := newFullTestEnv(t)
	uploadID, _, token := insertPendingUploadForStream(t, env, "public")

	// only 3 bytes — not enough for magic byte verification (needs 12)
	w := doPUT(t, env, uploadID, token, []byte{0xFF, 0xD8, 0xFF}, "image/jpeg")
	assertStatus(t, w, http.StatusUnprocessableEntity)
	assertErrorCode(t, w, "verification_failed")
}

func TestStream_ValidJPEG_PublicBucket(t *testing.T) {
	env := newFullTestEnv(t)
	uploadID, _, token := insertPendingUploadForStream(t, env, "public")

	w := doPUT(t, env, uploadID, token, validJPEGBytes(1024), "image/jpeg")
	assertStatus(t, w, http.StatusOK)

	body := decodeBody(t, w)
	if body["status"] != "verified" {
		t.Errorf("expected status verified, got %v", body["status"])
	}
	if body["public_url"] == nil {
		t.Error("expected public_url for public bucket")
	}
}

func TestStream_ValidJPEG_PrivateBucket(t *testing.T) {
	env := newFullTestEnv(t)
	uploadID, _, token := insertPendingUploadForStream(t, env, "private")

	w := doPUT(t, env, uploadID, token, validJPEGBytes(1024), "image/jpeg")
	assertStatus(t, w, http.StatusOK)

	body := decodeBody(t, w)
	if body["status"] != "verified" {
		t.Errorf("expected status verified, got %v", body["status"])
	}
	if body["public_url"] != nil {
		t.Error("expected public_url to be null for private bucket")
	}
}

func TestStream_ReplayAttack(t *testing.T) {
	env := newFullTestEnv(t)
	uploadID, _, token := insertPendingUploadForStream(t, env, "public")

	// first upload succeeds
	w := doPUT(t, env, uploadID, token, validJPEGBytes(1024), "image/jpeg")
	assertStatus(t, w, http.StatusOK)

	// second upload with same token must fail — upload is no longer pending
	w = doPUT(t, env, uploadID, token, validJPEGBytes(1024), "image/jpeg")
	if w.Code == http.StatusOK {
		t.Error("expected replay to be rejected, got 200")
	}
}

func TestStream_QuotaDeducted(t *testing.T) {
	env := newFullTestEnv(t)
	uploadID, _, token := insertPendingUploadForStream(t, env, "public")

	var projectID string
	env.db.QueryRow("SELECT project_id FROM uploads WHERE id = ?", uploadID).Scan(&projectID)

	var usedBefore int64
	env.db.QueryRow("SELECT used_bytes FROM projects WHERE id = ?", projectID).Scan(&usedBefore)

	w := doPUT(t, env, uploadID, token, validJPEGBytes(1024), "image/jpeg")
	assertStatus(t, w, http.StatusOK)

	var usedAfter int64
	env.db.QueryRow("SELECT used_bytes FROM projects WHERE id = ?", projectID).Scan(&usedAfter)

	if usedAfter <= usedBefore {
		t.Errorf("expected used_bytes to increase, before=%d after=%d", usedBefore, usedAfter)
	}
}