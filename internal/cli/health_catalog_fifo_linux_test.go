//go:build linux

package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const healthCatalogFIFOChildEnv = "DATAPAN_HEALTH_CATALOG_FIFO_CHILD"
const healthCatalogFIFOPathEnv = "DATAPAN_HEALTH_CATALOG_FIFO_PATH"

func TestReadBoundedFileDoesNotBlockWhenRegularPathBecomesFIFO(t *testing.T) {
	if os.Getenv(healthCatalogFIFOChildEnv) == "1" {
		path := os.Getenv(healthCatalogFIFOPathEnv)
		_, err := readBoundedFileWithOpener(path, healthCatalogMaxBytes, func(path string) (*os.File, error) {
			if err := os.Remove(path); err != nil {
				return nil, err
			}
			if err := unix.Mkfifo(path, 0o600); err != nil {
				return nil, err
			}
			return openBoundedCandidate(path)
		})
		if err == nil {
			t.Fatal("FIFO replacement was accepted as a bounded file")
		}
		return
	}

	path := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(path, []byte("regular file"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestReadBoundedFileDoesNotBlockWhenRegularPathBecomesFIFO$")
	command.Env = append(os.Environ(), healthCatalogFIFOChildEnv+"=1", healthCatalogFIFOPathEnv+"="+path)
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("bounded file open blocked on FIFO replacement: %s", output)
	}
	if err != nil {
		t.Fatalf("FIFO replacement subprocess failed: %v\n%s", err, output)
	}
}
