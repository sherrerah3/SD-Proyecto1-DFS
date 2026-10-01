package shell

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
