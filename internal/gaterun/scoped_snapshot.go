package gaterun

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/internal/pathguard"
	"github.com/MarcosAlves90/polis/v6/spec"
)

// scopedSnapshot captures worktree bytes, symlink/mode metadata, index entries,
// staging deltas, and Git visibility flags for an explicit closed input set.
// Untracked files include ignored files: ignoring an input must not hide changes.
// Never use this identity without a trusted complete-input declaration.
func scopedSnapshot(ctx context.Context, repo string, inputs []string) (string, error) {
	if len(inputs) == 0 {
		return "", errors.New("empty input scope")
	}
	specs := make([]string, 0, len(inputs))
	for _, path := range inputs {
		if err := spec.ValidateRepoRelativePath(path); err != nil {
			return "", err
		}
		if path == "." {
			return "", errors.New("repository-wide input must use global identity")
		}
		specs = append(specs, ":(literal)"+path)
	}
	// Git pathspecs cannot override the repository's boundary: git is only used
	// for enumeration and Git index metadata; every file path is checked below.
	gitBytes := func(args ...string) ([]byte, error) { return gitutil.Bytes(ctx, repo, nil, nil, args...) }
	stages, err := gitBytes(append([]string{"ls-files", "--stage", "-z", "--"}, specs...)...)
	if err != nil {
		return "", err
	}
	if bytes.Contains(stages, []byte("160000 ")) {
		return "", errors.New("scoped identity does not support submodules")
	}
	staged, err := gitBytes(append([]string{"diff", "--cached", "--raw", "--no-abbrev", "--no-renames", "--no-ext-diff", "--no-textconv", "--ita-invisible-in-index", "-z", "HEAD", "--"}, specs...)...)
	if err != nil {
		return "", err
	}
	flags, err := gitBytes(append([]string{"ls-files", "-v", "-z", "--"}, specs...)...)
	if err != nil {
		return "", err
	}
	// Without --exclude-standard, Git also enumerates ignored files inside the
	// declared inputs. These can influence the gate and must enter the identity.
	raw, err := gitBytes(append([]string{"ls-files", "--cached", "--others", "-z", "--"}, specs...)...)
	if err != nil {
		return "", err
	}
	set := map[string]bool{}
	for _, p := range bytes.Split(raw, []byte{0}) {
		if len(p) == 0 {
			continue
		}
		path := string(p)
		if err := spec.ValidateRepoRelativePath(path); err != nil {
			return "", err
		}
		matches := false
		for _, input := range inputs {
			if pathMatches(input, path) {
				matches = true
				break
			}
		}
		if !matches {
			return "", fmt.Errorf("unexpected path outside declared inputs: %q", path)
		}
		set[path] = true
	}
	if len(set) > 100000 {
		return "", errors.New("scoped identity exceeds file limit")
	}
	ordered := make([]string, 0, len(set))
	for path := range set {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	directories, err := scopedDirectories(repo, inputs)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	fmt.Fprintf(h, "scoped-input-v2 %q %q %q %q\n", inputs, stages, staged, flags)
	for _, entry := range directories {
		fmt.Fprintln(h, entry)
	}
	var total int64
	for _, path := range ordered {
		file := filepath.Join(repo, filepath.FromSlash(path))
		info, err := os.Lstat(file)
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(h, "%q missing\n", path)
			continue
		}
		if err != nil {
			return "", err
		}
		contained, err := pathguard.Contains(repo, file)
		if err != nil || !contained {
			return "", fmt.Errorf("scoped input %q resolves outside worktree", path)
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(file)
			if err != nil {
				return "", err
			}
			info, err = os.Stat(file)
			if err != nil {
				return "", err
			}
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("scoped input %q is not a regular file", path)
		}
		total += info.Size()
		if total > 256<<20 {
			return "", errors.New("scoped identity exceeds 256 MiB")
		}
		f, err := os.Open(file)
		if err != nil {
			return "", err
		}
		fh := sha256.New()
		n, copyErr := io.Copy(fh, io.LimitReader(f, info.Size()+1))
		closeErr := f.Close()
		if copyErr != nil || closeErr != nil {
			return "", errors.Join(copyErr, closeErr)
		}
		if n != info.Size() {
			return "", errors.New("scoped input changed while hashing")
		}
		fmt.Fprintf(h, "%q %t %q %x\n", path, info.Mode()&0111 != 0, link, fh.Sum(nil))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Git enumerates files but omits empty directories. A gate can depend on
// directory existence, so include in-scope directory topology and permissions
// in the scoped identity as well as file contents and Git metadata.
func scopedDirectories(repo string, inputs []string) ([]string, error) {
	const maxEntries = 100000
	seen := make(map[string]string)
	visited := 0
	for _, input := range inputs {
		root := filepath.Join(repo, filepath.FromSlash(input))
		if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		if inside, err := pathguard.Contains(repo, root); err != nil || !inside {
			return nil, fmt.Errorf("scoped input %q resolves outside worktree", input)
		}
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			visited++
			if visited > maxEntries {
				return errors.New("scoped identity exceeds directory traversal limit")
			}
			if !entry.IsDir() {
				return nil
			}
			if inside, err := pathguard.Contains(repo, path); err != nil || !inside {
				return fmt.Errorf("scoped directory %q resolves outside worktree", path)
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(repo, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			seen[relative] = fmt.Sprintf("directory %q mode=%#o", relative, info.Mode().Perm())
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	entries := make([]string, 0, len(paths))
	for _, path := range paths {
		entries = append(entries, seen[path])
	}
	return entries, nil
}
