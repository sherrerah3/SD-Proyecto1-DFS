package shell

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	figure "github.com/common-nighthawk/go-figure"
)

func newLogger() (*log.Logger, *os.File, error) {
	fallbackLogger := log.New(
		os.Stderr,
		"",
		log.LstdFlags,
	)

	if err := os.MkdirAll(
		shellLogDirectoryName,
		0755,
	); err != nil {
		return fallbackLogger, nil, err
	}

	logPath := filepath.Join(
		shellLogDirectoryName,
		shellLogFileName,
	)

	logFile, err := os.OpenFile(
		logPath,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		return fallbackLogger, nil, err
	}

	logger := log.New(
		logFile,
		"",
		log.LstdFlags,
	)

	return logger, logFile, nil
}

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

func reportShellError(s *shell, message string) {
	fullMessage := "error: " + message

	fmt.Println(fullMessage)

	if s.logger != nil {
		s.logger.Println(fullMessage)
	}
}

func printBanner() {
	figure.NewFigure(
		shellBannerText,
		"",
		true,
	).Print()
}
