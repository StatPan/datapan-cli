package cli

import "os"

func osWriteFile(name string, data []byte) error {
	return os.WriteFile(name, data, 0o600)
}

func osReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}
