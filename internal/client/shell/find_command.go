package shell

import "strings"

func findCommand(s *shell, name string) *command {
	normalizedName := strings.ToLower(name)

	for index := range s.commands {
		cmd := &s.commands[index]

		if cmd.name == normalizedName {
			return cmd
		}

		for _, alias := range cmd.aliases {
			if alias == normalizedName {
				return cmd
			}
		}
	}

	return nil
}
