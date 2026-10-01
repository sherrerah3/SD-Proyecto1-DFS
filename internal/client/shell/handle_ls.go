package shell

import "fmt"

func handleLs(s *shell, args []string) bool {
	fmt.Printf(
		errorNotImplemented,
		commandLs,
	)

	fmt.Println()

	return true
}
