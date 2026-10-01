package shell

import (
	"fmt"
	"strings"
)

func handleCommand(s *shell, input string) bool {
	parts := strings.Fields(input)

	if len(parts) == 0 {
		return true
	}

	name := strings.ToLower(parts[0])
	args := parts[1:]

	cmd := findCommand(s, name)

	if cmd == nil {
		reportShellError(
			s,
			fmt.Sprintf(
				errorUnknownCommand,
				name,
			),
		)

		fmt.Println()

		return true
	}

	if cmd.requiresAuth && s.session == nil {
		reportShellError(
			s,
			errorAuthentication,
		)

		fmt.Println()

		return true
	}

	continueShell := cmd.execute(s, args)

	if continueShell {
		fmt.Println()
	}

	return continueShell
}
