package shell

import "fmt"

func closeLogger(s *shell) {
	if s.logFile == nil {
		return
	}

	if err := s.logFile.Close(); err != nil {
		message := fmt.Sprintf(
			errorCloseLogger,
			err,
		)

		fmt.Println("error:", message)
	}
}
