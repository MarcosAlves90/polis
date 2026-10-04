package fileutil

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/pathguard"
)

type OutsideReadOptions struct {
	Max             int64
	OversizeMessage string
}

func ReadOutside(repo, filename string, opts OutsideReadOptions) ([]byte, error) {
	abs, err := filepath.Abs(filename)
	if err != nil {
		return nil, err
	}
	contained, err := pathguard.Contains(repo, abs)
	if err != nil {
		return nil, fmt.Errorf("resolve input boundary: %w", err)
	}
	if contained {
		return nil, errors.New("input must be outside target worktree")
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("input must be a regular file")
	}
	if info.Size() > opts.Max {
		if strings.Contains(opts.OversizeMessage, "%d") {
			return nil, fmt.Errorf(opts.OversizeMessage, opts.Max)
		}
		return nil, errors.New(opts.OversizeMessage)
	}
	return os.ReadFile(abs)
}

// WriteOutsideExclusive creates a new external report without overwriting a path.
func WriteOutsideExclusive(repo, filename string, data []byte) error {
	if filename == "" {
		return errors.New("output path is required")
	}
	if len(data) == 0 {
		return errors.New("external output must not be empty")
	}
	abs, err := filepath.Abs(filename)
	if err != nil {
		return fmt.Errorf("resolve external output path: %w", err)
	}
	contained, err := pathguard.Contains(repo, abs)
	if err != nil {
		return fmt.Errorf("resolve output boundary: %w", err)
	}
	if contained {
		return errors.New("output must be outside target worktree")
	}

	file, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create external output: %w", err)
	}
	created, statErr := file.Stat()
	writeErr := statErr
	if writeErr == nil {
		written, err := file.Write(data)
		if err != nil {
			writeErr = err
		} else if written != len(data) {
			writeErr = io.ErrShortWrite
		}
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		if created != nil {
			if current, err := os.Lstat(abs); err == nil && os.SameFile(created, current) {
				_ = os.Remove(abs)
			}
		}
		return fmt.Errorf("write external output: %w", writeErr)
	}
	return nil
}
