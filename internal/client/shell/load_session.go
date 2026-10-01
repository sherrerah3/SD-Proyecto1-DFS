package shell

import (
	"encoding/json"
	"errors"
	"os"
)

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
