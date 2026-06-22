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
	cmd, err := browserWorkflowCommand(script.Name(), args)
	if err != nil {
		_, _ = stderr.Write([]byte("python or uv not found; install uv or Python+Playwright to use browser-backed apply commands\n"))
		return exitRequest
	}
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

func browserWorkflowCommand(script string, args []string) (*exec.Cmd, error) {
	if uv, err := findUV(); err == nil {
		cmdArgs := append([]string{"run", "--with", "playwright", "python", script}, args...)
		return exec.Command(uv, cmdArgs...), nil
	}
	python, err := findPython()
	if err != nil {
		return nil, err
	}
	cmdArgs := append([]string{script}, args...)
	return exec.Command(python, cmdArgs...), nil
}

func findUV() (string, error) {
	if path, err := exec.LookPath("uv"); err == nil {
		return path, nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates := []string{
			home + "/.local/bin/uv",
			home + "/.cargo/bin/uv",
		}
		for _, candidate := range candidates {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}
	return "", os.ErrNotExist
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
