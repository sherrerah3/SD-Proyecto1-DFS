package shell

import "fmt"

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
