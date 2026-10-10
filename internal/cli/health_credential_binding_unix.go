//go:build darwin || linux

package cli

import (
	"os"
	"syscall"
)

func privateHealthCredentialBindingFile(file *os.File) bool {
	if file == nil {
		return false
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}
