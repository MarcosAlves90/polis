//go:build unix

package gaterun

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestManifestFIFOHelper(t *testing.T) {
	for i, arg := range os.Args {
		if arg != "--manifest-fifo" {
			continue
		}
		if _, err := Load(os.Args[i+1]); err == nil || !strings.Contains(err.Error(), "regular file") {
			os.Exit(1)
		}
		os.Exit(0)
	}
}

func TestManifestRejectsFIFOWithoutBlocking(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "run.json")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestManifestFIFOHelper$", "--", "--manifest-fifo", fifo)
	if err := cmd.Run(); err != nil {
		t.Fatalf("FIFO manifest must fail without waiting for a writer: %v (context: %v)", err, ctx.Err())
	}
}
