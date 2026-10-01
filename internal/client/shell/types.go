package shell

import (
	"bufio"
	"log"
	"os"
)

type shell struct {
	reader      *bufio.Reader
	commands    []command
	session     *session
	currentPath string
	logger      *log.Logger
	logFile     *os.File
}

type command struct {
	name         string
	aliases      []string
	description  string
	requiresAuth bool
	execute      func(*shell, []string) bool
}

type session struct {
	Username string `json:"username"`
	Token    string `json:"token"`
}
