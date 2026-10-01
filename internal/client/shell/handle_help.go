package shell

import "fmt"

func handleHelp(s *shell, args []string) bool {
	fmt.Println()
	fmt.Println(messageAvailableCommands)

	for _, cmd := range s.commands {
		if cmd.requiresAuth && s.session == nil {
			continue
		}

		fmt.Printf(
			"  %-10s %s\n",
			cmd.name,
			cmd.description,
		)
	}

	return true
}
