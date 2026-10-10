//go:build linux

package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestHealthExecutableSHA256UsesRunningImageAfterPathReplacement(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(original)

	placeholder, err := os.CreateTemp(filepath.Dir(executable), ".health-image-backup-*")
	if err != nil {
		t.Fatal(err)
	}
	backup := placeholder.Name()
	if err := placeholder.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(executable, backup); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(executable)
		if err := os.Rename(backup, executable); err != nil {
			t.Errorf("restore test executable path: %v", err)
		}
	})
	if err := os.WriteFile(executable, []byte("replacement image"), 0o700); err != nil {
		t.Fatal(err)
	}

	got, err := healthExecutableSHA256()
	if err != nil {
		t.Fatal(err)
	}
	if got != hex.EncodeToString(want[:]) {
		t.Fatalf("running-image digest changed after pathname replacement: got %s", got)
	}
}

func TestHealthExecutableSHA256FailsClosedOnUnavailableImage(t *testing.T) {
	_, err := healthExecutableSHA256WithOpen(func() (*os.File, error) {
		return nil, fs.ErrPermission
	})
	if !errors.Is(err, errHealthRunningImageAttestationUnavailable) {
		t.Fatalf("unreadable running image did not fail closed: %v", err)
	}
}

func TestHealthExecutableSHA256FromFileEnforcesRegularFileAndSize(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	if _, err := healthExecutableSHA256FromFile(reader, 1024); !errors.Is(err, errHealthRunningImageAttestationUnavailable) {
		t.Fatalf("non-regular executable source passed the guard: %v", err)
	}

	path := filepath.Join(t.TempDir(), "too-large.bin")
	if err := os.WriteFile(path, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := healthExecutableSHA256FromFile(file, 4); !errors.Is(err, errHealthRunningImageAttestationUnavailable) {
		t.Fatalf("oversized executable source passed the guard: %v", err)
	}
}
