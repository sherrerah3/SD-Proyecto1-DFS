package shell

import "fmt"

func handleCd(s *shell, args []string) bool {
	fmt.Printf(
		errorNotImplemented,
		commandCd,
	)

	fmt.Println()

	return true
}
