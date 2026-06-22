package cli

import (
	_ "embed"
	"fmt"
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
	runtime, err := resolveBrowserRuntime()
	if err != nil {
		_, _ = stderr.Write([]byte("python or uv not found; install uv or Python+Playwright to use browser-backed apply commands\n"))
		return exitRequest
	}
	if err := runtime.ensureChromium(stderr); err != nil {
		_, _ = stderr.Write([]byte(fmt.Sprintf("failed to prepare Playwright Chromium: %v\n", err)))
		return exitRequest
	}
	cmd := runtime.command(script.Name(), args)
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

type browserRuntime struct {
	uv     string
	python string
}

func resolveBrowserRuntime() (browserRuntime, error) {
	if uv, err := findUV(); err == nil {
		return browserRuntime{uv: uv}, nil
	}
	python, err := findPython()
	if err != nil {
		return browserRuntime{}, err
	}
	return browserRuntime{python: python}, nil
}

func (r browserRuntime) command(script string, args []string) *exec.Cmd {
	if r.uv != "" {
		cmdArgs := append([]string{"run", "--with", "playwright", "python", script}, args...)
		return exec.Command(r.uv, cmdArgs...)
	}
	cmdArgs := append([]string{script}, args...)
	return exec.Command(r.python, cmdArgs...)
}

func (r browserRuntime) ensureChromium(stderr io.Writer) error {
	var cmd *exec.Cmd
	if r.uv != "" {
		cmd = exec.Command(r.uv, "run", "--with", "playwright", "playwright", "install", "chromium")
	} else {
		cmd = exec.Command(r.python, "-m", "playwright", "install", "chromium")
	}
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	return cmd.Run()
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
