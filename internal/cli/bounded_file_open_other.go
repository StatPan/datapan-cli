//go:build !darwin && !linux

package cli

import "os"

func openBoundedCandidate(path string) (*os.File, error) {
	// On Windows, opening a named pipe with OPEN_EXISTING does not wait for a
	// server instance. The descriptor is checked for regular-file type before
	// any read, so a replacement pipe cannot supply catalog bytes.
	return os.Open(path)
}
