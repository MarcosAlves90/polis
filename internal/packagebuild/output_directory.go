package packagebuild

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/pathguard"
)

type packageOutput struct {
	repo         string
	requested    string
	requestedAbs string
	path         string
	parentPath   string
	dir          *os.File
	parent       *os.File
	suffix       []string
	owned        map[string]os.FileInfo
}

func preparePackageOutput(repo, requested string) (*packageOutput, error) {
	root, err := pathguard.Resolve(repo)
	if err != nil {
		return nil, fmt.Errorf("resolve repository path: %w", err)
	}
	requestedAbs, err := filepath.Abs(requested)
	if err != nil {
		return nil, fmt.Errorf("resolve package output path: %w", err)
	}
	inside, err := pathguard.Contains(root, requestedAbs)
	if err != nil {
		return nil, fmt.Errorf("validate delivery package output boundary: %w", err)
	}
	if inside {
		return nil, errors.New("delivery package output must be outside the target worktree")
	}
	resolved, err := pathguard.Resolve(requestedAbs)
	if err != nil {
		return nil, fmt.Errorf("validate delivery package output boundary: %w", err)
	}
	inside, err = pathguard.Contains(root, resolved)
	if err != nil {
		return nil, fmt.Errorf("validate delivery package output boundary: %w", err)
	}
	if inside {
		return nil, errors.New("delivery package output must be outside the target worktree")
	}

	ancestor := resolved
	reversedSuffix := make([]string, 0, 4)
	for {
		info, statErr := os.Stat(ancestor)
		if statErr == nil {
			if !info.IsDir() {
				return nil, fmt.Errorf("package output ancestor is not a directory: %s", ancestor)
			}
			break
		}
		if !errors.Is(statErr, fs.ErrNotExist) {
			return nil, fmt.Errorf("inspect package output ancestor %s: %w", ancestor, statErr)
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return nil, fmt.Errorf("find existing package output ancestor for %s", resolved)
		}
		reversedSuffix = append(reversedSuffix, filepath.Base(ancestor))
		ancestor = parent
	}
	ancestorInfo, err := os.Stat(ancestor)
	if err != nil {
		return nil, fmt.Errorf("inspect package output ancestor %s: %w", ancestor, err)
	}
	if !ancestorInfo.IsDir() {
		return nil, fmt.Errorf("package output ancestor is not a directory: %s", ancestor)
	}
	dir, err := openOutputDirectory(ancestor)
	if err != nil {
		return nil, fmt.Errorf("pin package output ancestor %s: %w", ancestor, err)
	}
	openedInfo, err := dir.Stat()
	if err != nil {
		dir.Close()
		return nil, fmt.Errorf("inspect pinned package output ancestor: %w", err)
	}
	currentInfo, err := os.Stat(ancestor)
	if err != nil || !os.SameFile(openedInfo, currentInfo) {
		dir.Close()
		if err != nil {
			return nil, fmt.Errorf("recheck package output ancestor %s: %w", ancestor, err)
		}
		return nil, errors.New("package output ancestor changed while it was being pinned")
	}
	inside, err = pathguard.Contains(root, ancestor)
	if err != nil || inside {
		dir.Close()
		if err != nil {
			return nil, fmt.Errorf("validate pinned package output ancestor: %w", err)
		}
		return nil, errors.New("delivery package output must be outside the target worktree")
	}

	suffix := make([]string, len(reversedSuffix))
	for i := range reversedSuffix {
		suffix[len(reversedSuffix)-1-i] = reversedSuffix[i]
	}
	output := &packageOutput{repo: root, requested: requested, requestedAbs: requestedAbs, path: resolved, parentPath: ancestor, suffix: suffix}
	if len(suffix) == 0 {
		output.dir = dir
	} else {
		output.parent = dir
	}
	return output, nil
}

func (p *packageOutput) Path() string {
	return p.path
}

func (p *packageOutput) FilePath(name string) string {
	return filepath.Join(p.requested, name)
}

func (p *packageOutput) ValidateRequested() error {
	resolved, err := pathguard.Resolve(p.requestedAbs)
	if err != nil {
		return fmt.Errorf("validate delivery package output boundary: %w", err)
	}
	inside, err := pathguard.Contains(p.repo, resolved)
	if err != nil {
		return fmt.Errorf("validate delivery package output boundary: %w", err)
	}
	if inside {
		return errors.New("delivery package output must be outside the target worktree")
	}
	if !sameOutputPath(resolved, p.path) {
		return errors.New("package output path changed during validation")
	}
	return p.ValidatePinnedDirectory()
}

func (p *packageOutput) ValidatePinnedDirectory() error {
	handle := p.dir
	expectedPath := p.path
	if handle == nil {
		handle = p.parent
		expectedPath = p.parentPath
	}
	if handle == nil {
		return errors.New("package output directory handle is not open")
	}
	openedInfo, err := handle.Stat()
	if err != nil {
		return fmt.Errorf("inspect pinned package output directory: %w", err)
	}
	currentInfo, err := os.Stat(expectedPath)
	if err != nil {
		return fmt.Errorf("pinned package output directory moved or changed: %w", err)
	}
	if !os.SameFile(openedInfo, currentInfo) {
		return errors.New("pinned package output directory moved or changed")
	}
	inside, err := pathguard.Contains(p.repo, expectedPath)
	if err != nil {
		return fmt.Errorf("validate pinned package output boundary: %w", err)
	}
	if inside {
		return errors.New("delivery package output must be outside the target worktree")
	}
	return nil
}

func (p *packageOutput) Ensure() error {
	if p.dir != nil {
		return p.ValidatePinnedDirectory()
	}
	if p.parent == nil || len(p.suffix) == 0 {
		return errors.New("package output directory is not prepared")
	}
	if err := p.ValidateRequested(); err != nil {
		return err
	}

	current := p.parent
	ownedCurrent := false
	for _, component := range p.suffix {
		child, err := openOutputChild(current, component)
		if err != nil && isOutputNotExist(err) {
			if mkdirErr := makeOutputChildDirectory(current, component); mkdirErr != nil && !isOutputExists(mkdirErr) {
				if ownedCurrent {
					current.Close()
				}
				return fmt.Errorf("create package output directory component %s: %w", component, mkdirErr)
			}
			child, err = openOutputChild(current, component)
		}
		if err != nil {
			if ownedCurrent {
				current.Close()
			}
			return fmt.Errorf("open package output directory component %s: %w", component, err)
		}
		if ownedCurrent {
			current.Close()
		}
		current = child
		ownedCurrent = true
	}
	p.dir = current
	if p.parent != nil {
		_ = p.parent.Close()
		p.parent = nil
	}
	if err := p.ValidateRequested(); err != nil {
		return err
	}
	return nil
}

func (p *packageOutput) CreateTemp(pattern string) (*os.File, string, error) {
	if err := p.Ensure(); err != nil {
		return nil, "", err
	}
	if strings.Count(pattern, "*") != 1 || strings.ContainsAny(pattern, `/\\`) {
		return nil, "", fmt.Errorf("invalid temporary package filename pattern %q", pattern)
	}
	for attempts := 0; attempts < 8; attempts++ {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, "", fmt.Errorf("generate temporary package filename: %w", err)
		}
		name := strings.Replace(pattern, "*", hex.EncodeToString(random[:]), 1)
		file, err := createOutputFile(p.dir, name, 0o600, true)
		if isOutputExists(err) {
			continue
		}
		if err != nil {
			return nil, "", fmt.Errorf("create candidate archive: %w", err)
		}
		if err := p.trackOwned(name, file); err != nil {
			_ = file.Close()
			return nil, "", fmt.Errorf("track candidate archive: %w", err)
		}
		if err := p.ValidatePinnedDirectory(); err != nil {
			_ = file.Close()
			return nil, "", errors.Join(fmt.Errorf("create candidate archive: %w", err), cleanupPackageOutputFiles(p, name))
		}
		return file, name, nil
	}
	return nil, "", errors.New("could not allocate unique candidate archive name")
}

func (p *packageOutput) CreateFileExclusive(name string, mode os.FileMode) (*os.File, error) {
	if err := validateOutputName(name); err != nil {
		return nil, err
	}
	if err := p.Ensure(); err != nil {
		return nil, err
	}
	file, err := createOutputFile(p.dir, name, mode, false)
	if err != nil {
		if isOutputExists(err) {
			return nil, fmt.Errorf("output artifact already exists: %s", p.FilePath(name))
		}
		return nil, fmt.Errorf("create final artifact: %w", err)
	}
	if err := p.trackOwned(name, file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("track final artifact: %w", err)
	}
	if err := p.ValidatePinnedDirectory(); err != nil {
		_ = file.Close()
		return nil, errors.Join(fmt.Errorf("create final artifact: %w", err), cleanupPackageOutputFiles(p, name))
	}
	return file, nil
}

func (p *packageOutput) Remove(name string) error {
	if err := validateOutputName(name); err != nil {
		return err
	}
	if p.dir == nil {
		return errors.New("package output directory is not open")
	}
	expected, tracked := p.owned[name]
	if !tracked {
		return fmt.Errorf("refuse to remove untracked package output %s", name)
	}
	current, err := openOutputFileForIdentity(p.dir, name)
	if isOutputNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect package output before removal %s: %w", name, err)
	}
	currentInfo, err := current.Stat()
	_ = current.Close()
	if err != nil {
		return fmt.Errorf("inspect package output identity %s: %w", name, err)
	}
	if !os.SameFile(expected, currentInfo) {
		return fmt.Errorf("refuse to remove replaced package output %s", name)
	}
	if err := removeOutputFile(p.dir, name); err != nil && !isOutputNotExist(err) {
		return fmt.Errorf("remove package output %s: %w", name, err)
	}
	return nil
}

func (p *packageOutput) trackOwned(name string, file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if p.owned == nil {
		p.owned = make(map[string]os.FileInfo)
	}
	p.owned[name] = info
	return nil
}

// ValidateFinalArtifact compares the published file against the verified
// candidate using the pinned directory, including the file's original identity.
func (p *packageOutput) ValidateFinalArtifact(name, expectedSHA256 string) error {
	if err := validateOutputName(name); err != nil {
		return err
	}
	if err := p.ValidateRequested(); err != nil {
		return err
	}
	expected, ok := p.owned[name]
	if !ok {
		return fmt.Errorf("final package output %s is not tracked", name)
	}
	file, err := openOutputFileForIdentity(p.dir, name)
	if err != nil {
		return fmt.Errorf("open final package for verification: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat final package for verification: %w", err)
	}
	if !os.SameFile(expected, info) {
		return errors.New("final package output was replaced after copying")
	}
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return fmt.Errorf("hash final package for verification: %w", err)
	}
	if hex.EncodeToString(h.Sum(nil)) != expectedSHA256 {
		return errors.New("final package output digest changed after copying")
	}
	return p.ValidateRequested()
}

func cleanupPackageOutputFiles(output *packageOutput, names ...string) error {
	if output == nil {
		return errors.New("package output directory is not prepared")
	}
	var cleanupErr error
	for _, name := range names {
		if name == "" {
			continue
		}
		if err := output.Remove(name); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("cleanup package output %s: %w", name, err))
		}
	}
	return cleanupErr
}

func (p *packageOutput) Close() error {
	var closeErr error
	if p.dir != nil {
		closeErr = p.dir.Close()
		p.dir = nil
	}
	if p.parent != nil {
		if err := p.parent.Close(); closeErr == nil {
			closeErr = err
		}
		p.parent = nil
	}
	return closeErr
}

func validateOutputName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") || strings.IndexByte(name, 0) >= 0 || filepath.Base(name) != name {
		return fmt.Errorf("invalid package output filename %q", name)
	}
	return nil
}

func sameOutputPath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func copyExclusive(source, target string) (returnErr error) {
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open verified archive: %w", err)
	}
	defer in.Close()
	if _, err := in.Seek(0, 0); err != nil {
		return fmt.Errorf("seek verified archive: %w", err)
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve final artifact path: %w", err)
	}
	parentPath, err := pathguard.Resolve(filepath.Dir(targetAbs))
	if err != nil {
		return fmt.Errorf("resolve final artifact directory: %w", err)
	}
	parent, err := openOutputDirectory(parentPath)
	if err != nil {
		return fmt.Errorf("open final artifact directory: %w", err)
	}
	defer parent.Close()
	name := filepath.Base(targetAbs)
	if err := validateOutputName(name); err != nil {
		return err
	}
	out, err := createOutputFile(parent, name, 0o644, false)
	if err != nil {
		if isOutputExists(err) {
			return fmt.Errorf("output artifact already exists: %s", target)
		}
		return fmt.Errorf("create final artifact: %w", err)
	}
	ok := false
	defer func() {
		_ = out.Close()
		if !ok {
			if removeErr := removeOutputFile(parent, name); removeErr != nil && !isOutputNotExist(removeErr) {
				returnErr = errors.Join(returnErr, fmt.Errorf("remove failed final artifact: %w", removeErr))
			}
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy final artifact: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close final artifact: %w", err)
	}
	ok = true
	return nil
}
