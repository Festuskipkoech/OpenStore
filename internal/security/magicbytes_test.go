package security_test

import (
	"testing"

	"openstore/internal/security"
)

func makeHeader(size int, fill ...byte) []byte {
	buf := make([]byte, size)
	copy(buf, fill)
	return buf
}

func jpegHeader() []byte { return makeHeader(12, 0xFF, 0xD8, 0xFF, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00) }
func pngHeader() []byte { return makeHeader(12, 0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x00) }
func webpHeader() []byte { return makeHeader(12, 0x52, 0x49, 0x46, 0x46, 0x00, 0x00, 0x00, 0x00, 0x57, 0x45, 0x42, 0x50) }
func gifHeader() []byte { return makeHeader(12, 0x47, 0x49, 0x46, 0x38, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00) }
func mp4Header() []byte { return makeHeader(12, 0x00, 0x00, 0x00, 0x00, 0x66, 0x74, 0x79, 0x70, 0x00, 0x00, 0x00, 0x00) }
func webmHeader() []byte { return makeHeader(12, 0x1A, 0x45, 0xDF, 0xA3, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00) }
func mp3Header() []byte { return makeHeader(12, 0x49, 0x44, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00) }
func mp3SyncHeader() []byte { return makeHeader(12, 0xFF, 0xFB, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00) }
func wavHeader() []byte { return makeHeader(12, 0x52, 0x49, 0x46, 0x46, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00) }
func oggHeader() []byte { return makeHeader(12, 0x4F, 0x67, 0x67, 0x53, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00) }
func flacHeader() []byte { return makeHeader(12, 0x66, 0x4C, 0x61, 0x43, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00) }
func pdfHeader() []byte { return makeHeader(12, 0x25, 0x50, 0x44, 0x46, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00) }

func assertPass(t *testing.T, mime string, header []byte) {
	t.Helper()
	if err := security.VerifyMagicBytes(mime, header); err != nil {
		t.Errorf("%s: expected pass, got error: %v", mime, err)
	}
}

func assertFail(t *testing.T, mime string, header []byte) {
	t.Helper()
	if err := security.VerifyMagicBytes(mime, header); err == nil {
		t.Errorf("%s: expected failure, got nil", mime)
	}
}

func TestMagicBytes_JPEG_Valid(t *testing.T) { assertPass(t, "image/jpeg", jpegHeader()) }
func TestMagicBytes_JPEG_Tampered(t *testing.T) { assertFail(t, "image/jpeg", pngHeader()) }

func TestMagicBytes_PNG_Valid(t *testing.T) { assertPass(t, "image/png", pngHeader()) }
func TestMagicBytes_PNG_Tampered(t *testing.T) { assertFail(t, "image/png", makeHeader(12, 0x00)) }

func TestMagicBytes_WebP_Valid(t *testing.T) { assertPass(t, "image/webp", webpHeader()) }
func TestMagicBytes_WebP_RIFFOnlyIsNotEnough(t *testing.T) {
	// WAV shares the RIFF prefix — WebP requires WEBP at offset 8 too.
	assertFail(t, "image/webp", wavHeader())
}

func TestMagicBytes_GIF_Valid(t *testing.T) { assertPass(t, "image/gif", gifHeader()) }
func TestMagicBytes_GIF_Tampered(t *testing.T) { assertFail(t, "image/gif", jpegHeader()) }

func TestMagicBytes_MP4_Valid(t *testing.T) { assertPass(t, "video/mp4", mp4Header()) }
func TestMagicBytes_MP4_FtypAtOffsetZeroFails(t *testing.T) {
	// ftyp at offset 0 must not match — the box starts at offset 4.
	header := makeHeader(12, 0x66, 0x74, 0x79, 0x70, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)
	assertFail(t, "video/mp4", header)
}

func TestMagicBytes_WebM_Valid(t *testing.T) { assertPass(t, "video/webm", webmHeader()) }
func TestMagicBytes_WebM_Tampered(t *testing.T) { assertFail(t, "video/webm", jpegHeader()) }

func TestMagicBytes_MP3_ID3_Valid(t *testing.T) { assertPass(t, "audio/mpeg", mp3Header()) }
func TestMagicBytes_MP3_SyncWord_Valid(t *testing.T) { assertPass(t, "audio/mpeg", mp3SyncHeader()) }
func TestMagicBytes_MP3_Tampered(t *testing.T) { assertFail(t, "audio/mpeg", jpegHeader()) }

func TestMagicBytes_WAV_Valid(t *testing.T) { assertPass(t, "audio/wav", wavHeader()) }
func TestMagicBytes_WAV_Tampered(t *testing.T) { assertFail(t, "audio/wav", jpegHeader()) }

func TestMagicBytes_OGG_Valid(t *testing.T) { assertPass(t, "audio/ogg", oggHeader()) }
func TestMagicBytes_OGG_Tampered(t *testing.T) { assertFail(t, "audio/ogg", jpegHeader()) }

func TestMagicBytes_FLAC_Valid(t *testing.T) { assertPass(t, "audio/flac", flacHeader()) }
func TestMagicBytes_FLAC_Tampered(t *testing.T) { assertFail(t, "audio/flac", jpegHeader()) }

func TestMagicBytes_PDF_Valid(t *testing.T) { assertPass(t, "application/pdf", pdfHeader()) }
func TestMagicBytes_PDF_Tampered(t *testing.T) { assertFail(t, "application/pdf", jpegHeader()) }

func TestMagicBytes_EmptyBytes(t *testing.T) {
	assertFail(t, "image/jpeg", []byte{})
}

func TestMagicBytes_InsufficientBytes(t *testing.T) {
	// 3 bytes — not enough for types requiring 8+ bytes.
	assertFail(t, "image/png", []byte{0x89, 0x50, 0x4E})
}

func TestMagicBytes_UnrecognisedMIME(t *testing.T) {
	assertFail(t, "application/octet-stream", jpegHeader())
}