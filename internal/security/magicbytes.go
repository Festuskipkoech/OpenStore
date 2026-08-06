package security

import (
	"bytes"
	"fmt"
)

// HeaderSize is the number of bytes read from every upload stream for magic byte verification.
// 12 bytes covers all signatures, including the WebP WEBP marker at offset 8.
const HeaderSize = 12

// ErrMagicMismatch is returned when the file header does not match the declared MIME type's signature.
type ErrMagicMismatch struct {
	MIMEType string
	Reason  string
}

func (e *ErrMagicMismatch) Error() string {
	return fmt.Sprintf("magic bytes do not match declared MIME type %s: %s", e.MIMEType, e.Reason)
}

// VerifyMagicBytes checks the first HeaderSize bytes of a file against the known signature for mimeType.
// Special cases: MP4 ftyp box starts at offset 4 (not 0); WebP requires RIFF at offset 0 AND WEBP at offset 8
// to distinguish it from WAV, which shares the RIFF prefix.
func VerifyMagicBytes(mimeType string, header []byte) error {
	if len(header) < HeaderSize {
		return fmt.Errorf("header too short: need %d bytes, got %d", HeaderSize, len(header))
	}

	switch mimeType {
	case "image/jpeg":
		return checkSignature(mimeType, header, 0, []byte{0xFF, 0xD8, 0xFF})
	case "image/png":
		return checkSignature(mimeType, header, 0, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})
	case "image/webp":
		if err := checkSignature(mimeType, header, 0, []byte{0x52, 0x49, 0x46, 0x46}); err != nil {
			return err
		}
		return checkSignature(mimeType, header, 8, []byte{0x57, 0x45, 0x42, 0x50})
	case "image/gif":
		return checkSignature(mimeType, header, 0, []byte{0x47, 0x49, 0x46, 0x38})
	case "video/mp4":
		// Bytes 0-3 are the box length; the ftyp signature is at offset 4.
		return checkSignature(mimeType, header, 4, []byte{0x66, 0x74, 0x79, 0x70})
	case "video/webm":
		return checkSignature(mimeType, header, 0, []byte{0x1A, 0x45, 0xDF, 0xA3})
	case "audio/mpeg":
		if matchesAt(header, 0, []byte{0x49, 0x44, 0x33}) ||
			matchesAt(header, 0, []byte{0xFF, 0xFB}) ||
			matchesAt(header, 0, []byte{0xFF, 0xF3}) ||
			matchesAt(header, 0, []byte{0xFF, 0xF2}) {
			return nil
		}
		return &ErrMagicMismatch{MIMEType: mimeType, Reason: "no ID3 tag or MPEG sync word found at offset 0"}
	case "audio/wav":
		return checkSignature(mimeType, header, 0, []byte{0x52, 0x49, 0x46, 0x46})
	case "audio/ogg":
		return checkSignature(mimeType, header, 0, []byte{0x4F, 0x67, 0x67, 0x53})
	case "audio/flac":
		return checkSignature(mimeType, header, 0, []byte{0x66, 0x4C, 0x61, 0x43})
	case "application/pdf":
		return checkSignature(mimeType, header, 0, []byte{0x25, 0x50, 0x44, 0x46})
	default:
		return fmt.Errorf("unrecognised MIME type for magic byte verification: %s", mimeType)
	}
}

func checkSignature(mimeType string, header []byte, offset int, sig []byte) error {
	if !matchesAt(header, offset, sig) {
		return &ErrMagicMismatch{
			MIMEType: mimeType,
			Reason:   fmt.Sprintf("expected %X at offset %d, got %X", sig, offset, safeSlice(header, offset, offset+len(sig))),
		}
	}
	return nil
}

func matchesAt(data []byte, offset int, sig []byte) bool {
	if offset+len(sig) > len(data) {
		return false
	}
	return bytes.Equal(data[offset:offset+len(sig)], sig)
}

func safeSlice(data []byte, start, end int) []byte {
	if start >= len(data) {
		return nil
	}
	if end > len(data) {
		end = len(data)
	}
	return data[start:end]
}