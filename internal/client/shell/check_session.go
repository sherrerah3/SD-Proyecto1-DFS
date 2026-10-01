package shell

import "fmt"

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
