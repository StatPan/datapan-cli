package cli

import (
	_ "embed"
	"io"
	"os"
	"os/exec"
	"runtime"
)

//go:embed data_go_kr_apply.py
var dataGoKrApplyScript string

func runBrowserWorkflow(args []string, stdout, stderr io.Writer) int {
	python, err := findPython()
	if err != nil {
		_, _ = stderr.Write([]byte("python not found; install Python and Playwright to use browser-backed apply commands\n"))
		return exitRequest
	}
	script, err := os.CreateTemp("", "datapan-data-go-kr-apply-*.py")
	if err != nil {
		_, _ = stderr.Write([]byte(err.Error() + "\n"))
		return exitRequest
	}
	defer os.Remove(script.Name())
	if _, err := script.WriteString(dataGoKrApplyScript); err != nil {
		_ = script.Close()
		_, _ = stderr.Write([]byte(err.Error() + "\n"))
		return exitRequest
	}
	if err := script.Close(); err != nil {
		_, _ = stderr.Write([]byte(err.Error() + "\n"))
		return exitRequest
	}
	cmdArgs := append([]string{script.Name()}, args...)
	cmd := exec.Command(python, cmdArgs...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		_, _ = stderr.Write([]byte(err.Error() + "\n"))
		return exitRequest
	}
	return exitOK
}

func findPython() (string, error) {
	names := []string{"python", "python3"}
	if runtime.GOOS == "windows" {
		names = append([]string{"py"}, names...)
	}
	for _, name := range names {
		path, err := exec.LookPath(name)
		if err == nil {
			return path, nil
		}
	}
	return "", os.ErrNotExist
}
