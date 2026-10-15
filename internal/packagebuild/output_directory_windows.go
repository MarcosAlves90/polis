//go:build windows

package packagebuild

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const outputDirectoryShare = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE

func openOutputDirectory(path string) (*os.File, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	abs = filepath.Clean(abs)
	volume := filepath.VolumeName(abs)
	if volume == "" {
		return nil, fmt.Errorf("package output path has no volume: %s", abs)
	}
	root := volume + string(filepath.Separator)
	dir, err := openWindowsDirectoryByName(root)
	if err != nil {
		return nil, err
	}
	relative := strings.TrimPrefix(abs, root)
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		if component == ".." {
			dir.Close()
			return nil, fmt.Errorf("non-canonical directory component in %q", abs)
		}
		child, err := openOutputChild(dir, component)
		dir.Close()
		if err != nil {
			return nil, err
		}
		dir = child
	}
	return dir, nil
}

func openWindowsDirectoryByName(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, windows.FILE_LIST_DIRECTORY|windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE, outputDirectoryShare, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	if err := verifyWindowsDirectoryHandle(handle); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func openOutputChild(parent *os.File, name string) (*os.File, error) {
	handle, err := ntCreateOutputHandle(parent, name, windows.FILE_LIST_DIRECTORY|windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE, windows.FILE_OPEN, windows.FILE_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_OPEN_FOR_BACKUP_INTENT|windows.FILE_SYNCHRONOUS_IO_NONALERT, windows.FILE_ATTRIBUTE_DIRECTORY)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), name)
	if err := verifyWindowsDirectoryHandle(handle); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func makeOutputChildDirectory(parent *os.File, name string) error {
	handle, err := ntCreateOutputHandle(parent, name, windows.FILE_LIST_DIRECTORY|windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE, windows.FILE_CREATE, windows.FILE_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_OPEN_FOR_BACKUP_INTENT|windows.FILE_SYNCHRONOUS_IO_NONALERT, windows.FILE_ATTRIBUTE_DIRECTORY)
	if err != nil {
		return err
	}
	if err := verifyWindowsDirectoryHandle(handle); err != nil {
		windows.CloseHandle(handle)
		return err
	}
	return windows.CloseHandle(handle)
}

func createOutputFile(parent *os.File, name string, mode os.FileMode, readWrite bool) (*os.File, error) {
	_ = mode
	access := uint32(windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE | windows.SYNCHRONIZE)
	if !readWrite {
		access = windows.FILE_GENERIC_WRITE | windows.FILE_READ_ATTRIBUTES | windows.SYNCHRONIZE
	}
	handle, err := ntCreateOutputHandle(parent, name, access, windows.FILE_CREATE, windows.FILE_NON_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT, windows.FILE_ATTRIBUTE_NORMAL)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), name), nil
}

func removeOutputFile(parent *os.File, name string) error {
	handle, err := ntCreateOutputHandle(parent, name, windows.DELETE|windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT, windows.FILE_ATTRIBUTE_NORMAL)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	var iosb windows.IO_STATUS_BLOCK
	deleteFlag := byte(1)
	return windows.NtSetInformationFile(handle, &iosb, &deleteFlag, uint32(unsafe.Sizeof(deleteFlag)), windows.FileDispositionInformation)
}

func ntCreateOutputHandle(parent *os.File, name string, access, disposition, options, attributes uint32) (windows.Handle, error) {
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return windows.InvalidHandle, err
	}
	oa := &windows.OBJECT_ATTRIBUTES{
		RootDirectory: windows.Handle(parent.Fd()),
		ObjectName:    objectName,
		Attributes:    windows.OBJ_CASE_INSENSITIVE,
	}
	oa.Length = uint32(unsafe.Sizeof(*oa))
	var iosb windows.IO_STATUS_BLOCK
	var handle windows.Handle
	err = windows.NtCreateFile(&handle, access, oa, &iosb, nil, attributes, outputDirectoryShare, disposition, options, 0, 0)
	if err != nil {
		return windows.InvalidHandle, err
	}
	return handle, nil
}

func verifyWindowsDirectoryHandle(handle windows.Handle) error {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("package output directory contains a reparse point")
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return errors.New("package output path is not a directory")
	}
	return nil
}

func isOutputNotExist(err error) bool {
	return err == windows.STATUS_OBJECT_NAME_NOT_FOUND || err == windows.STATUS_OBJECT_PATH_NOT_FOUND || errors.Is(err, os.ErrNotExist)
}

func isOutputExists(err error) bool {
	return err == windows.STATUS_OBJECT_NAME_COLLISION || errors.Is(err, os.ErrExist)
}
