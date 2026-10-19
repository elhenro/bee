package apply_patch

import (
	"context"
	"os"
	"testing"
)

func TestDeleteVerifiesHunks(t *testing.T) {
	dir := runInDir(t)
	writeFile(t, "gone.txt", "alpha\nbeta\n")
	wrong := "--- a/gone.txt\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-WRONG1\n-WRONG2\n"
	res, err := New(dir).Run(context.Background(), map[string]any{"patch": wrong})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("delete with mismatching content must fail, got: %s", res.Content)
	}
	if _, err := os.Stat("gone.txt"); err != nil {
		t.Fatalf("file must survive a mismatching delete: %v", err)
	}

	partial := "--- a/gone.txt\n+++ /dev/null\n@@ -1,1 +0,0 @@\n-alpha\n"
	res, _ = New(dir).Run(context.Background(), map[string]any{"patch": partial})
	if !res.IsError {
		t.Fatalf("delete leaving content behind must fail, got: %s", res.Content)
	}
	if _, err := os.Stat("gone.txt"); err != nil {
		t.Fatalf("file must survive a partial delete: %v", err)
	}
}
