package shell

import (
	"log"
	"os"
	"path/filepath"
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
