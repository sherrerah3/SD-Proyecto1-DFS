package client

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type savedSession struct {
	Token string `json:"token"`
}

func LoadSession(path string) (Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Session{}, err
	}

	var saved savedSession
	if err := json.Unmarshal(data, &saved); err != nil {
		return Session{}, err
	}

	if saved.Token == "" {
		return Session{}, errors.New("session file contains an empty token")
	}

	return Session{Token: saved.Token}, nil
}

func SaveSession(path string, session Session) error {
	if session.Token == "" {
		return errors.New("cannot save an empty token")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	data, err := json.Marshal(savedSession{Token: session.Token})
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

func DeleteSession(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
