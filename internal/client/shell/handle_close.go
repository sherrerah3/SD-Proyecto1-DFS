package shell

import "fmt"

func handleClose(s *shell, args []string) bool {
	fmt.Printf(
		errorNotImplemented,
		commandClose,
	)

	fmt.Println()

	return true
}
