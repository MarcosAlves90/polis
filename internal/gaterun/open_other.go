//go:build !unix

package gaterun

import "os"

func openManifest(filename string) (*os.File, error) {
	return os.Open(filename)
}
