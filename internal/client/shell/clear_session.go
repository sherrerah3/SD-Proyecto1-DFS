package shell

import (
	"errors"
	"os"
)

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
