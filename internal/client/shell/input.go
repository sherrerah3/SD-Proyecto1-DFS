package shell

import (
	"errors"
	"fmt"
	"io"

	"github.com/hitraa/readline"
)

func readInput(s *shell, editor *readline.Editor) (string, error) {
	prompt := fmt.Sprintf("%s> ", shellVarProjectName)

	if s.session != nil {
		prompt = fmt.Sprintf(
			"%s$%s:%s> ",
			s.session.Username,
			shellVarProjectName,
			s.currentPath,
		)
	}

	input, err := editor.ReadLine(prompt)

	if errors.Is(err, readline.ErrInterrupt) {
		// Ctrl+C conserva el comportamiento de salida actual.
		return commandExit, nil
	}

	if errors.Is(err, io.EOF) {
		return "", fmt.Errorf("%s", errorInputClosed)
	}

	if err != nil {
		return "", err
	}

	return input, nil
}
