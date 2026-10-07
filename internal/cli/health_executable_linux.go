//go:build linux

package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
)

const healthExecutableMaxBytes = 128 << 20

var errHealthRunningImageAttestationUnavailable = errors.New("running-image attestation is unavailable")

// On Linux, /proc/self/exe resolves to the executable inode used by this
// process, even when its original pathname has since been atomically replaced.
// Hash the opened descriptor rather than the pathname returned by os.Executable.
func healthExecutableSHA256() (string, error) {
	return healthExecutableSHA256WithOpen(func() (*os.File, error) {
		return os.Open("/proc/self/exe")
	})
}

func healthExecutableSHA256WithOpen(open func() (*os.File, error)) (string, error) {
	file, err := open()
	if err != nil {
		return "", fmt.Errorf("%w: %v", errHealthRunningImageAttestationUnavailable, err)
	}
	defer file.Close()
	return healthExecutableSHA256FromFile(file, healthExecutableMaxBytes)
}

func healthExecutableSHA256FromFile(file *os.File, maxBytes int64) (string, error) {
	if file == nil || maxBytes < 1 {
		return "", errHealthRunningImageAttestationUnavailable
	}
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() < 1 || before.Size() > maxBytes {
		return "", errHealthRunningImageAttestationUnavailable
	}

	digest := sha256.New()
	n, err := io.Copy(digest, io.LimitReader(file, maxBytes+1))
	if err != nil || n != before.Size() || n > maxBytes {
		return "", errHealthRunningImageAttestationUnavailable
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return "", errHealthRunningImageAttestationUnavailable
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
