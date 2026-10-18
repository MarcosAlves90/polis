//go:build unix

package packagebuild

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const outputDirectoryOpenFlags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW

func openOutputDirectory(path string) (*os.File, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	abs = filepath.Clean(abs)
	fd, err := unix.Open(string(filepath.Separator), outputDirectoryOpenFlags, 0)
	if err != nil {
		return nil, err
	}
	for _, component := range strings.Split(strings.TrimPrefix(abs, string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		if component == ".." {
			unix.Close(fd)
			return nil, fmt.Errorf("non-canonical directory component in %q", abs)
		}
		next, err := unix.Openat(fd, component, outputDirectoryOpenFlags, 0)
		if err != nil {
			unix.Close(fd)
			return nil, err
		}
		if err := unix.Close(fd); err != nil {
			unix.Close(next)
			return nil, err
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), abs), nil
}

func openOutputChild(parent *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, outputDirectoryOpenFlags, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func makeOutputChildDirectory(parent *os.File, name string) error {
	return unix.Mkdirat(int(parent.Fd()), name, 0o755)
}

func createOutputFile(parent *os.File, name string, mode os.FileMode, readWrite bool) (*os.File, error) {
	flags := unix.O_CREAT | unix.O_EXCL | unix.O_CLOEXEC | unix.O_NOFOLLOW
	if readWrite {
		flags |= unix.O_RDWR
	} else {
		flags |= unix.O_WRONLY
	}
	fd, err := unix.Openat(int(parent.Fd()), name, flags, uint32(mode.Perm()))
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func removeOutputFile(parent *os.File, name string) error {
	return unix.Unlinkat(int(parent.Fd()), name, 0)
}

func openOutputFileForIdentity(parent *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func isOutputNotExist(err error) bool {
	return errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ENOTDIR)
}

func isOutputExists(err error) bool {
	return errors.Is(err, unix.EEXIST)
}
