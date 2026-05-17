package main

import "runtime"

// exeSuffix returns ".exe" on Windows, empty elsewhere. Tests that build the
// bee binary into a TempDir need this so exec.Command can find the resulting
// file — `go build -o bin/bee` on Windows produces `bin/bee.exe`, and absolute
// paths passed to exec.Command on Windows do not fall through to add the
// extension automatically.
func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
