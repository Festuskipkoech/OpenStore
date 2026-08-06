package handlers
 
import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
 
	"github.com/chai2010/webp"
	_ "golang.org/x/image/webp"
)

// sanitiseImage decodes and re-encodes the image, stripping all metadata as a
// natural consequence of the decode/encode cycle. The stdlib encoders write only
// pixel data — EXIF, XMP, ICC profiles, and comments are never carried through.

func sanitiseImage(data []byte, mimeType string) ([]byte, error) {
	img, err := decodeImage(data)
	if err != nil {
		return nil, fmt.Errorf("decode failed (%s): %w", mimeType, err)
	}

	return encodeImage(img, mimeType)
}

func decodeImage(data []byte) (image.Image, error) {
	// golang.org/x/image/webp is registered via blank import above and
	// is picked up by image.Decode for "image/webp" format strings.
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("image.Decode: %w", err)
	}
	return img, nil
}


func encodeImage(img image.Image, mimeType string) ([]byte, error) {
	var buf bytes.Buffer
 
	switch mimeType {
	case "image/jpeg":
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 92}); err != nil {
			return nil, fmt.Errorf("jpeg encode: %w", err)
		}
 
	case "image/png":
		if err := png.Encode(&buf, img); err != nil {
			return nil, fmt.Errorf("png encode: %w", err)
		}
 
	case "image/gif":
		if err := gif.Encode(&buf, img, nil); err != nil {
			return nil, fmt.Errorf("gif encode: %w", err)
		}
 
	case "image/webp":
		if err := webp.Encode(&buf, img, &webp.Options{Lossless: false, Quality: 92}); err != nil {
			return nil, fmt.Errorf("webp encode: %w", err)
		}
 
	default:
		return nil, fmt.Errorf("unsupported image MIME type for sanitisation: %s", mimeType)
	}
 
	return buf.Bytes(), nil
}