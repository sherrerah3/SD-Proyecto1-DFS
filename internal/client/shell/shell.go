package shell

import (
	"bufio"
	"dfsha/internal/client/api"
	"fmt"
	"os"

	"github.com/hitraa/readline"
)

func Run() {
	s, err := newShell()
	if err != nil {
		message := fmt.Sprintf(
			errorInitializeLogger,
			err,
		)
		fmt.Println("error:", message)
		return
	}

	defer closeLogger(s)

	handleClear(s, nil)

	printBanner()
	fmt.Println()

	if err := checkSession(s); err != nil {
		reportShellError(
			s,
			fmt.Sprintf(
				errorCheckSession,
				err,
			),
		)
	}

	if s.session == nil {
		fmt.Println()
		fmt.Println(messageNoActiveSession)
		fmt.Println(messageUseHelp)
	} else {
		fmt.Printf(
			messageLoggedInAs,
			s.session.Username,
		)
	}

	fmt.Println()

	editor, err := newLineEditor()
	if err != nil {
		reportShellError(
			s,
			fmt.Sprintf(errorInitializeLogger, err),
		)
		return
	}
	defer editor.Close()

	for {
		input, err := readInput(s, editor)
		if err != nil {
			reportShellError(
				s,
				fmt.Sprintf(
					errorReadInput,
					err,
				),
			)

			fmt.Println(messageShellTerminated)
			return
		}

		if input == "" {
			continue
		}

		if !handleCommand(s, input) {
			return
		}
	}
}

func newShell() (*shell, error) {
	logger, logFile, err := newLogger()

	s := &shell{
		reader:      bufio.NewReader(os.Stdin),
		commands:    defaultCommands,
		session:     nil,
		currentPath: shellDefaultPath,
		metanode:    api.New(),
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

func newLineEditor() (*readline.Editor, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}

	historyDir := configDir + string(os.PathSeparator) + "dfsha"

	if err := os.MkdirAll(historyDir, 0700); err != nil {
		return nil, err
	}

	historyFile := historyDir + string(os.PathSeparator) + "history"

	return readline.New(readline.Config{
		Prompt:      shellVarProjectName + "> ",
		HistoryFile: historyFile,
		MaxHistory:  1000,
	})
}
