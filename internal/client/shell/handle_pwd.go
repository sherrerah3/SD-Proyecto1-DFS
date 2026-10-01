package shell

import "fmt"

func handlePwd(s *shell, args []string) bool {
	fmt.Println(s.currentPath)
	fmt.Println()

	return true
}
