package shell

import (
	"bufio"
	"fmt"
	"os"
)

func newShell() (*shell, error) {
	logger, logFile, err := newLogger()

	s := &shell{
		reader:      bufio.NewReader(os.Stdin),
		commands:    defaultCommands,
		session:     nil,
		currentPath: shellDefaultPath,
		logger:      logger,
		logFile:     logFile,
	}

	if err != nil {
		return s, fmt.Errorf(
			errorInitializeLogger,
			err,
		)
	}

	return s, nil
}
