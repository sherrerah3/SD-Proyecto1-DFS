package shell

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"

	"golang.org/x/term"
)

func readInput(s *shell) (input string, err error) {
	fileDescriptor := int(os.Stdin.Fd())

	if !term.IsTerminal(fileDescriptor) {
		line, readErr := s.reader.ReadString('\n')
		if readErr != nil {
			return "", readErr
		}

		return strings.TrimSpace(line), nil
	}

	state, err := term.MakeRaw(fileDescriptor)
	if err != nil {
		return "", err
	}

	defer func() {
		restoreErr := term.Restore(
			fileDescriptor,
			state,
		)

		if restoreErr != nil {
			err = errors.Join(err, restoreErr)
		}
	}()

	characters := make([]rune, 0)

	for {
		character, _, readErr := s.reader.ReadRune()
		if readErr != nil {
			return "", readErr
		}

		switch character {
		case '\r', '\n':
			if _, writeErr := fmt.Fprint(
				os.Stdout,
				terminalNewLine,
			); writeErr != nil {
				return "", writeErr
			}

			return strings.TrimSpace(
				string(characters),
			), nil

		case terminalCtrlC:
			if _, writeErr := fmt.Fprint(
				os.Stdout,
				terminalCtrlCDisplay,
			); writeErr != nil {
				return "", writeErr
			}

			return commandExit, nil

		case terminalCtrlD:
			if len(characters) == 0 {
				return "", fmt.Errorf(
					errorInputClosed,
				)
			}

		case terminalBackspace, terminalDelete:
			if len(characters) == 0 {
				continue
			}

			characters = characters[:len(characters)-1]

			if _, writeErr := fmt.Fprint(
				os.Stdout,
				"\b \b",
			); writeErr != nil {
				return "", writeErr
			}

		case terminalCtrlL:
			if _, writeErr := fmt.Fprint(
				os.Stdout,
				terminalNewLine,
			); writeErr != nil {
				return "", writeErr
			}

			return commandClear, nil

		default:
			if !unicode.IsPrint(character) {
				continue
			}

			characters = append(
				characters,
				character,
			)

			if _, writeErr := fmt.Fprint(
				os.Stdout,
				string(character),
			); writeErr != nil {
				return "", writeErr
			}
		}
	}
}
