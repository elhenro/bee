package read

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"  // register decoders for DecodeConfig
	_ "image/jpeg" // register decoders for DecodeConfig
	_ "image/png"  // register decoders for DecodeConfig
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// providers reject bigger pictures outright
const maxImageBytes = 5 << 20

var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true}

// NotePrefix leads the result text of an attached picture so the TUI can show
// it instead of collapsing it to a line count.
const NotePrefix = "image · "

// readImage hands a picture file to the model as an image block. ok is false
// when path is not a supported picture, so the caller falls through to the
// text path (and its binary refusal). The type comes from the bytes, not the
// extension: a renamed file is not sent to a provider as an image.
func readImage(path string, size int64) (res tools.Result, ok bool) {
	if !imageExts[strings.ToLower(filepath.Ext(path))] {
		return res, false
	}
	if size > maxImageBytes {
		return tools.Result{
			Content: fmt.Sprintf("image %s is %s, over the %s limit; downscale it first", path, humanSize(size), humanSize(maxImageBytes)),
			IsError: true,
		}, true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return tools.Result{Content: fmt.Sprintf("read %s: %v", path, err), IsError: true}, true
	}
	mt := http.DetectContentType(data)
	if mt != "image/png" && mt != "image/jpeg" && mt != "image/gif" && mt != "image/webp" {
		return res, false
	}
	note := NotePrefix + filepath.Base(path) + " · " + mt
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		note += fmt.Sprintf(" · %d×%d", cfg.Width, cfg.Height)
	}
	note += " · " + humanSize(int64(len(data))) + " · attached below"
	return tools.Result{
		Content: note,
		Images:  []types.ContentBlock{{Type: types.BlockImage, MediaType: mt, Data: data}},
	}, true
}

func humanSize(n int64) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d KB", (n+1023)/1024)
}
