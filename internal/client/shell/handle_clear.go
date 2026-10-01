package shell

import (
	"fmt"
	"os"
)

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
