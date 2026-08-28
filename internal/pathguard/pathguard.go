package pathguard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Contains reports whether candidate resolves to root or a descendant of root.
// Existing symlinks are resolved. Missing leaf components are canonicalized by
// resolving the nearest existing ancestor and reattaching the missing suffix.
func Contains(root, candidate string) (bool, error) {
	rootCanonical, err := canonical(root)
	if err != nil {
		return false, fmt.Errorf("canonicalize root: %w", err)
	}
	candidateCanonical, err := canonical(candidate)
	if err != nil {
		return false, fmt.Errorf("canonicalize candidate: %w", err)
	}
	rel, err := filepath.Rel(rootCanonical, candidateCanonical)
	if err != nil {
		return false, err
	}
	if rel == "." {
		return true, nil
	}
	if rel == ".." || filepath.IsAbs(rel) {
		return false, nil
	}
	prefix := ".." + string(filepath.Separator)
	return len(rel) < len(prefix) || rel[:len(prefix)] != prefix, nil
}

func canonical(path string) (string, error) {
	return canonicalWithAbs(path, filepath.Abs)
}

func canonicalWithAbs(path string, makeAbs func(string) (string, error)) (string, error) {
	abs, err := makeAbs(path)
	if err != nil {
		return "", err
	}
	current := filepath.Clean(abs)
	suffix := make([]string, 0, 4)
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}
