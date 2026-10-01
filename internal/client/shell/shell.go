package shell

import (
	"fmt"
)

func Run() {
	s, err := newShell()
	if err != nil {
		message := fmt.Sprintf(
			errorInitializeLogger,
			err,
		)
		fmt.Println("error:", message)
		return
	}

	defer closeLogger(s)

	handleClear(s, nil)

	printBanner()

	if err := checkSession(s); err != nil {
		reportShellError(
			s,
			fmt.Sprintf(
				errorCheckSession,
				err,
			),
		)
	}

	if s.session == nil {
		fmt.Println()
		fmt.Println(messageNoActiveSession)
		fmt.Println(messageUseHelp)
	} else {
		fmt.Printf(
			messageLoggedInAs,
			s.session.Username,
		)
	}

	fmt.Println()

	for {
		fmt.Printf(
			"%s> ",
			shellVarProjectName,
		)

		input, err := readInput(s)
		if err != nil {
			reportShellError(
				s,
				fmt.Sprintf(
					errorReadInput,
					err,
				),
			)

			fmt.Println(messageShellTerminated)

			return
		}

		if input == "" {
			continue
		}

		if !handleCommand(s, input) {
			return
		}
	}
}
