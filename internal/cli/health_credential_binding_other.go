//go:build !darwin && !linux && !windows

package cli

import "os"

func privateHealthCredentialBindingFile(*os.File) bool { return false }
