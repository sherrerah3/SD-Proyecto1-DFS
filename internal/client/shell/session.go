package shell

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func checkSession(s *shell) error {
	currentSession, err := loadSession()
	if err != nil {
		return fmt.Errorf("failed to load session: %w", err)
	}

	if currentSession == nil {
		s.session = nil
		return nil
	}

	if !verifySession(currentSession) {
		if err := clearSession(); err != nil {
			return fmt.Errorf("failed to clear invalid session: %w", err)
		}

		s.session = nil
		return nil
	}

	s.session = currentSession

	return nil
}

func loadSession() (*session, error) {
	path, err := getSessionPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, err
	}

	var currentSession session

	if err := json.Unmarshal(data, &currentSession); err != nil {
		return nil, err
	}

	return &currentSession, nil
}

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

func clearSession() error {
	path, err := getSessionPath()
	if err != nil {
		return err
	}

	err = os.Remove(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return err
	}

	return nil
}

func verifySession(currentSession *session) bool {
	if currentSession == nil {
		return false
	}

	if currentSession.Username == "" {
		return false
	}

	if currentSession.Token == "" {
		return false
	}

	return true
}

func getSessionPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	directory := filepath.Join(configDir, sessionDirectoryName)

	return filepath.Join(directory, sessionFileName), nil
}
