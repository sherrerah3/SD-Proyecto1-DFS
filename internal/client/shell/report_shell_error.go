package shell

import "fmt"

func reportShellError(s *shell, message string) {
	fullMessage := "error: " + message

	fmt.Println(fullMessage)

	if s.logger != nil {
		s.logger.Println(fullMessage)
	}
}
