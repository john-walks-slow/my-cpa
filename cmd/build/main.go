//go:build ignore

// Build orchestrator for my-cpa-stats-plugin.
// Usage: go run cmd/build/main.go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

type target struct {
	goos   string
	goarch string
	ext    string
}

var targets = []target{
	{"linux", "amd64", ".so"},
	{"linux", "arm64", ".so"},
	{"darwin", "arm64", ".dylib"},
	{"darwin", "amd64", ".dylib"},
	{"windows", "amd64", ".dll"},
}

func main() {
	for _, t := range targets {
		outDir := filepath.Join("bin", t.goos, t.goarch)
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			fatal(err)
		}
		outFile := filepath.Join(outDir, "my-cpa-stats-plugin"+t.ext)
		fmt.Printf("building %s/%s -> %s\n", t.goos, t.goarch, outFile)

		cmd := exec.Command("go", "build", "-buildmode=c-shared", "-o", outFile, "./plugin")
		cmd.Env = append(os.Environ(), "GOOS="+t.goos, "GOARCH="+t.goarch, "CGO_ENABLED=1")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "WARN: %s/%s build failed (cross-compile requires C toolchain): %v\n", t.goos, t.goarch, err)
			if t.goos == runtime.GOOS && t.goarch == runtime.GOARCH {
				fatal(err)
			}
		}
		_ = os.Remove(filepath.Join(outDir, "my-cpa-stats-plugin.h"))
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "FATAL:", err)
	os.Exit(1)
}
