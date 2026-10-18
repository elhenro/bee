package read

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func writePNG(t *testing.T, path string, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestReadImageAttachesBlock(t *testing.T) {
	p := filepath.Join(t.TempDir(), "Shot.PNG")
	want := writePNG(t, p, 40, 30)
	res, err := New().Run(context.Background(), map[string]any{"path": p})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if len(res.Images) != 1 {
		t.Fatalf("want 1 image, got %d", len(res.Images))
	}
	b := res.Images[0]
	if b.Type != types.BlockImage || b.MediaType != "image/png" || !bytes.Equal(b.Data, want) {
		t.Fatalf("bad block: type=%s media=%s len=%d", b.Type, b.MediaType, len(b.Data))
	}
	for _, s := range []string{NotePrefix + "Shot.PNG", "image/png", "40×30", "attached below"} {
		if !strings.Contains(res.Content, s) {
			t.Fatalf("note missing %q: %q", s, res.Content)
		}
	}
}

func TestReadImageTooBig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.png")
	if err := os.WriteFile(p, make([]byte, maxImageBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ := New().Run(context.Background(), map[string]any{"path": p})
	if !res.IsError || len(res.Images) != 0 || !strings.Contains(res.Content, "limit") {
		t.Fatalf("want size refusal, got %+v", res)
	}
}

// a text file with a picture extension is read as text; a binary one that is
// not really a picture keeps the binary refusal
func TestReadImageExtensionDoesNotTrustBytes(t *testing.T) {
	dir := t.TempDir()
	txt := filepath.Join(dir, "notes.png")
	os.WriteFile(txt, []byte("just words\n"), 0o644)
	res, _ := New().Run(context.Background(), map[string]any{"path": txt})
	if res.IsError || len(res.Images) != 0 || !strings.Contains(res.Content, "just words") {
		t.Fatalf("text file should read as text: %+v", res)
	}
	bin := filepath.Join(dir, "fake.jpg")
	os.WriteFile(bin, append([]byte{0, 1, 2, 3}, make([]byte, 64)...), 0o644)
	res, _ = New().Run(context.Background(), map[string]any{"path": bin})
	if !res.IsError || len(res.Images) != 0 || !strings.Contains(res.Content, "binary") {
		t.Fatalf("fake picture should be refused as binary: %+v", res)
	}
}
