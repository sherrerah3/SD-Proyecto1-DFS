package shell

import "fmt"

func handleWrite(s *shell, args []string) bool {
	fmt.Printf(
		errorNotImplemented,
		commandWrite,
	)

	fmt.Println()

	return true
}
