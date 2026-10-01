//go:build unix

package gaterun

import (
	"os"
	"syscall"
)

// A record replaced with a FIFO between stat and open must not block waiting
// for a writer. Load validates the opened descriptor before reading anything.
func openManifest(filename string) (*os.File, error) {
	return os.OpenFile(filename, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
