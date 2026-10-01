package shell

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func saveSession(currentSession *session) error {
	path, err := getSessionPath()
	if err != nil {
		return err
	}

	directory := filepath.Dir(path)

	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(currentSession, "", "    ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}

	return nil
}
