package shell

import (
	"fmt"
	"os"
)

func handleHelp(s *shell, args []string) bool {
	fmt.Println()
	fmt.Println(messageAvailableCommands)

	for _, cmd := range s.commands {
		if cmd.requiresAuth && s.session == nil {
			continue
		}

		fmt.Printf(
			"  %-10s %s\n",
			cmd.name,
			cmd.description,
		)
	}

	return true
}

func handleClear(s *shell, args []string) bool {
	_, err := fmt.Fprint(
		os.Stdout,
		terminalClearSequence,
	)

	if err != nil {
		reportShellError(
			s,
			fmt.Sprintf(
				errorClearTerminal,
				err,
			),
		)

		fmt.Println()
	}

	return true
}

func handleExit(s *shell, args []string) bool {
	return false
}
