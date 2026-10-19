package shell

import (
	"dfsha/internal/client/api"
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

	if !verifySession(currentSession) {
		if currentSession != nil {
			if err := clearSession(); err != nil {
				return fmt.Errorf("failed to clear invalid session: %w", err)
			}
		}

		s.session = nil
		return nil
	}

	if err := s.metanode.ValidateSession(currentSession.Token); err != nil {
		var apiErr *api.Error

		if errors.As(err, &apiErr) && apiErr.Status == 401 {
			if clearErr := clearSession(); clearErr != nil {
				return fmt.Errorf("failed to clear expired session: %w", clearErr)
			}

			s.session = nil
			return nil
		}

		return fmt.Errorf("failed to validate session: %w", err)
	}

	s.session = currentSession
	return nil
}

// ensureSessionValid checks the active session before an authenticated
// command runs. If the server rejects the token with HTTP 401, the
// expired session is cleared and the user is asked to log in again.
func ensureSessionValid(s *shell) bool {
	if s.session == nil {
		return false
	}

	err := s.metanode.ValidateSession(s.session.Token)
	if err == nil {
		return true
	}

	var apiErr *api.Error
	if errors.As(err, &apiErr) && apiErr.Status == 401 {
		clearErr := clearSession()
		s.session = nil

		if clearErr != nil {
			reportShellError(
				s,
				fmt.Sprintf("failed to clear expired session: %v", clearErr),
			)
		}

		message := "Session expired. Please log in again using 'login'."
		fmt.Println(message)

		if s.logger != nil {
			s.logger.Println(message)
		}

		return false
	}

	reportShellError(
		s,
		fmt.Sprintf("could not validate session: %v", err),
	)

	return false
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
