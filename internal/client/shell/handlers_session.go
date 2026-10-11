package shell

import "fmt"

func handleLogin(s *shell, args []string) bool {
	fmt.Printf(
		errorNotImplemented,
		commandLogin,
	)

	fmt.Println()

	return true
}

func handleRegister(s *shell, args []string) bool {
	fmt.Printf(
		errorNotImplemented,
		commandRegister,
	)

	fmt.Println()

	return true
}

func handleLogout(s *shell, args []string) bool {
	if err := clearSession(); err != nil {
		reportShellError(
			s,
			fmt.Sprintf(
				errorClearSession,
				err,
			),
		)

		fmt.Println()

		return true
	}

	s.session = nil

	fmt.Println(messageLoggedOut)
	fmt.Println()

	return true
}
