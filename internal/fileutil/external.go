package fileutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MarcosAlves90/polis/v5/internal/pathguard"
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
