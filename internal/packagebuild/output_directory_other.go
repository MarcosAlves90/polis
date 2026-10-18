//go:build !unix && !windows

package packagebuild

import (
	"errors"
	"fmt"
	"os"
)

var errSafePackageOutputUnsupported = errors.New("safe package output operations are unsupported on this platform")

func openOutputDirectory(string) (*os.File, error) {
	return nil, errSafePackageOutputUnsupported
}

func openOutputChild(*os.File, string) (*os.File, error) {
	return nil, errSafePackageOutputUnsupported
}

func makeOutputChildDirectory(*os.File, string) error {
	return errSafePackageOutputUnsupported
}

func createOutputFile(*os.File, string, os.FileMode, bool) (*os.File, error) {
	return nil, errSafePackageOutputUnsupported
}

func removeOutputFile(*os.File, string) error {
	return errSafePackageOutputUnsupported
}

func openOutputFileForIdentity(*os.File, string) (*os.File, error) {
	return nil, errSafePackageOutputUnsupported
}

func isOutputNotExist(error) bool {
	return false
}

func isOutputExists(error) bool {
	return false
}

func unsupportedOutputError(operation string) error {
	return fmt.Errorf("%s: %w", operation, errSafePackageOutputUnsupported)
}
