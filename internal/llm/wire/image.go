package wire

import (
	"encoding/base64"

	"github.com/elhenro/bee/internal/types"
)

// shared image-block helpers. Every provider translator encodes the same raw
// bytes the same way and defaults the same media type; the only thing that
// differs is the envelope shape each wire format wraps it in. Keep that shared
// logic here so a fix (a new default, a different encoding) lands once.

// imageMediaType returns the block's media type, defaulting to image/png.
func imageMediaType(b types.ContentBlock) string {
	if b.MediaType == "" {
		return "image/png"
	}
	return b.MediaType
}

// imageBase64 standard-base64-encodes the block's raw image bytes.
func imageBase64(b types.ContentBlock) string {
	return base64.StdEncoding.EncodeToString(b.Data)
}

// imageDataURL renders the block as a data: URL — the shape OpenAI-style
// image_url fields expect.
func imageDataURL(b types.ContentBlock) string {
	return "data:" + imageMediaType(b) + ";base64," + imageBase64(b)
}
