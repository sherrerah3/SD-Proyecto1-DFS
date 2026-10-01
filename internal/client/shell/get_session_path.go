package shell

import (
	"os"
	"path/filepath"
)

func getSessionPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	directory := filepath.Join(configDir, sessionDirectoryName)

	return filepath.Join(directory, sessionFileName), nil
}
