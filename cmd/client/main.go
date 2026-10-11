package main

import (
	"errors"

	"flag"

	"fmt"

	"os"

	"path"

	"regexp"

	"strings"

	"unicode/utf8"

	"dfsha/common"

	"dfsha/internal/client"

	"github.com/chzyer/readline"

	"golang.org/x/term"
)

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9]{1,16}$`)

func main() {

	debug := flag.Bool("debug", false, "Use HTTP for local development")

	production := flag.Bool("production", false, "Use HTTPS")

	addr := flag.String("addr", "", "MetaNode address")

	sessionPath := flag.String("session", "data/session", "Session file path")

	flag.Parse()

	if *debug == *production {

		fmt.Fprintln(os.Stderr, "Specify exactly one of --debug or --production")

		os.Exit(1)

	}

	if *addr == "" {

		fmt.Fprintln(os.Stderr, "--addr is required")

		os.Exit(1)

	}

	scheme := "https"

	if *debug {

		scheme = "http"

	}

	runShell(client.New(scheme+"://"+*addr), *sessionPath)

}

func runShell(auth client.AuthService, sessionPath string) {

	var session *client.Session

	currentPath := "/"

	saved, loadErr := client.LoadSession(sessionPath)

	switch {

	case loadErr == nil:

		restored, err := auth.RestoreSession(saved.Token)

		switch {

		case err == nil:

			session = &restored

			fmt.Println("Restored and validated session.")

		case errors.Is(err, client.ErrInvalidSession):

			if deleteErr := client.DeleteSession(sessionPath); deleteErr != nil {

				fmt.Fprintln(os.Stderr, "Could not delete invalid session:", deleteErr)

			}

			fmt.Println("Saved session is invalid or expired.")

		default:

			fmt.Fprintln(os.Stderr, "Could not validate saved session:", err)

			fmt.Println("Starting unauthenticated.")

		}

	case errors.Is(loadErr, os.ErrNotExist):

	default:

		fmt.Fprintln(os.Stderr, "Could not load saved session:", loadErr)

	}

	rl, err := readline.NewEx(&readline.Config{

		Prompt: "dfsha (unauthenticated)> ",

		HistoryFile: "",

		InterruptPrompt: "^C",

		EOFPrompt: "exit",
	})

	if err != nil {

		fmt.Fprintln(os.Stderr, "Could not initialize terminal:", err)

		return

	}

	defer rl.Close()

	fmt.Println("DFSha Client")

	fmt.Println("Type 'help' to see available commands.")

	fmt.Println("Use Ctrl+C to cancel input.")

	for {

		prompt := "dfsha (unauthenticated)> "

		if session != nil {

			prompt = session.User + "@dfsha:" + currentPath + "> "

		}

		rl.SetPrompt(prompt)

		line, err := rl.Readline()

		if err != nil {

			if errors.Is(err, readline.ErrInterrupt) {

				fmt.Println("Input cancelled.")

				continue

			}

			return

		}

		args := strings.Fields(line)

		if len(args) == 0 {

			continue

		}

		command := strings.ToLower(args[0])

		switch command {

		case "exit":

			if len(args) != 1 {

				fmt.Println("Usage: exit")

				continue

			}

			return

		case "help":

			if len(args) != 1 {

				fmt.Println("Usage: help")

				continue

			}

			printHelp(session != nil)

		case "login", "register":

			if len(args) != 1 {

				fmt.Printf("Usage: %s\n", command)

				continue

			}

			if session != nil {

				fmt.Println("Already logged in. Use 'logout' first.")

				continue

			}

			user, err := readUsername()

			if err != nil {

				if errors.Is(err, readline.ErrInterrupt) {

					fmt.Println("Operation cancelled.")

				} else {

					fmt.Fprintln(os.Stderr, "Could not read username:", err)

				}

				continue

			}

			var password string

			if command == "register" {

				password, err = readRegistrationPassword()

			} else {

				password, err = readPassword("Password: ")

			}

			if err != nil {

				if errors.Is(err, readline.ErrInterrupt) {

					fmt.Println("Operation cancelled.")

				} else {

					fmt.Fprintln(os.Stderr, "Could not read password:", err)

				}

				continue

			}

			var newSession client.Session

			if command == "login" {

				newSession, err = auth.Login(user, password)

			} else {

				newSession, err = auth.Register(user, password)

			}

			password = ""

			if err != nil {

				fmt.Printf("%s failed: %v\n", command, err)

				continue

			}

			if err := client.SaveSession(sessionPath, newSession); err != nil {

				fmt.Fprintln(os.Stderr, "Could not save session:", err)

				fmt.Println("Warning: session will not persist after exit.")

			}

			session = &newSession

			currentPath = "/"

			fmt.Println("Authentication successful.")

		case "logout":

			if len(args) != 1 {

				fmt.Println("Usage: logout")

				continue

			}

			if session == nil {

				fmt.Println("Not logged in.")

				continue

			}

			if err := client.DeleteSession(sessionPath); err != nil {

				fmt.Fprintln(os.Stderr, "Could not delete saved session:", err)

				fmt.Println("Still logged in.")

				continue

			}

			session = nil

			currentPath = "/"

			fmt.Println("Logged out.")

		default:

			if session == nil {

				fmt.Println("Access denied. Log in first.")

				fmt.Println("Available commands: login, register, help, exit")

				continue

			}

			switch command {

			case "ls":
				recursive := false
				remotePath := currentPath
				pathSeen := false
				valid := true

				for _, arg := range args[1:] {
					if arg == "-r" {
						if recursive {
							valid = false
							break
						}
						recursive = true
						continue
					}
					if strings.HasPrefix(arg, "-") || pathSeen {
						valid = false
						break
					}
					remotePath = resolvePath(currentPath, arg)
					pathSeen = true
				}

				if !valid {
					fmt.Println("Usage: ls [path] [-r]")
					continue
				}

				if recursive {
					if err := listPath(auth, session.Token, remotePath, ""); err != nil {
						fmt.Printf("ls failed: %v\n", err)
					}
					continue
				}

				entries, err := auth.ListDirectory(session.Token, remotePath)
				if err != nil {
					fmt.Printf("ls failed: %v\n", err)
					continue
				}
				for _, entry := range entries {
					if entry.Kind == "directory" {
						fmt.Println(entry.Name + "/")
					} else {
						fmt.Println(entry.Name)
					}
				}

			case "mkdir":

				if len(args) != 2 {

					fmt.Println("Usage: mkdir <path>")

					continue

				}

				remotePath := resolvePath(currentPath, args[1])

				if err := auth.MakeDirectory(session.Token, remotePath); err != nil {

					fmt.Printf("mkdir failed: %v\n", err)

					continue

				}

				fmt.Println("Directory created.")

			case "rmdir":
				recursive := false
				force := false
				remoteArg := ""

				for _, arg := range args[1:] {
					if strings.HasPrefix(arg, "-") {
						if len(arg) < 2 {
							remoteArg = arg
							break
						}
						for _, option := range arg[1:] {
							switch option {
							case 'r':
								recursive = true
							case 'f':
								force = true
							default:
								remoteArg = ""
								fmt.Println("Usage: rmdir [-f] [-r] <path>")
								remoteArg = "\x00"
							}
						}
						if remoteArg == "\x00" {
							break
						}
					} else if remoteArg == "" {
						remoteArg = arg
					} else {
						remoteArg = "\x00"
						break
					}
				}

				if remoteArg == "\x00" || remoteArg == "" {
					fmt.Println("Usage: rmdir [-f] [-r] <path>")
					continue
				}

				remotePath := resolvePath(currentPath, remoteArg)
				if remotePath == "/" {
					fmt.Println("Cannot remove the root directory.")
					continue
				}
				if remotePath == currentPath || strings.HasPrefix(currentPath, strings.TrimRight(remotePath, "/")+"/") {
					fmt.Println("Cannot remove the current directory or one of its ancestors.")
					continue
				}

				if err := auth.RemoveDirectoryOptions(session.Token, remotePath, recursive, force); err != nil {
					fmt.Printf("rmdir failed: %v\n", err)
					continue
				}
				fmt.Println("Directory removed.")

			case "cd":

				if len(args) != 2 {

					fmt.Println("Usage: cd <path>")

					continue

				}

				newPath := resolvePath(currentPath, args[1])

				// ListDirectory also verifies that the target exists

				// and is a directory, provided the server implements

				// directory-only listing as expected.

				_, err := auth.ListDirectory(session.Token, newPath)

				if err != nil {

					fmt.Printf("cd failed: %v\n", err)

					continue

				}

				currentPath = newPath

			default:

				fmt.Printf("Unknown command: %s\n", command)

				fmt.Println("Type 'help' to see available commands.")

			}

		}

	}

}

// resolvePath converts a remote absolute or relative path into a normalized

// absolute path. It does not access the local filesystem.

func resolvePath(currentPath, target string) string {

	if target == "" {

		return currentPath

	}

	if strings.HasPrefix(target, "/") {

		return normalizeRemotePath(target)

	}

	return normalizeRemotePath(path.Join(currentPath, target))

}

func normalizeRemotePath(remotePath string) string {

	cleaned := path.Clean("/" + strings.TrimLeft(remotePath, "/"))

	if cleaned == "." || cleaned == "" {

		return "/"

	}

	return cleaned

}

func readUsername() (string, error) {

	rl, err := readline.NewEx(&readline.Config{

		Prompt: "Username: ",

		HistoryFile: "",

		InterruptPrompt: "^C",
	})

	if err != nil {

		return "", err

	}

	defer rl.Close()

	for {

		user, err := rl.Readline()

		if err != nil {

			return "", err

		}

		if !usernamePattern.MatchString(user) {

			fmt.Println("Invalid username format. Use 1-16 alphanumeric characters (A-Z, a-z, 0-9).")

			continue

		}

		return user, nil

	}

}

func readPassword(prompt string) (string, error) {

	fd := int(os.Stdin.Fd())

	if !term.IsTerminal(fd) {

		return "", fmt.Errorf("password input requires an interactive terminal")

	}

	for {

		fmt.Print(prompt)

		password, err := term.ReadPassword(fd)

		fmt.Println()

		if err != nil {

			clearBytes(password)

			return "", err

		}

		if !validPassword(string(password)) {

			clearBytes(password)

			fmt.Printf(

				"Invalid password format. Password must be %d-%d bytes long.\n",

				common.MinPasswordLength,

				common.MaxPasswordLength,
			)

			continue

		}

		result := string(password)

		clearBytes(password)

		return result, nil

	}

}

func readRegistrationPassword() (string, error) {

	for {

		password, err := readPassword("Password: ")

		if err != nil {

			return "", err

		}

		confirmation, err := readPassword("Confirm password: ")

		if err != nil {

			password = ""

			return "", err

		}

		if password != confirmation {

			password = ""

			confirmation = ""

			fmt.Println("Passwords do not match. Try again.")

			continue

		}

		confirmation = ""

		return password, nil

	}

}

func validPassword(password string) bool {

	return len(password) >= common.MinPasswordLength &&

		len(password) <= common.MaxPasswordLength &&

		utf8.ValidString(password)

}

func clearBytes(value []byte) {

	for i := range value {

		value[i] = 0

	}

}

func listPath(auth client.AuthService, token, remotePath, prefix string) error {
	entries, err := auth.ListDirectory(token, remotePath)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		name := entry.Name
		childPath := path.Join(remotePath, entry.Name)
		if entry.Kind == "directory" {
			fmt.Printf("%s%s/\n", prefix, name)
			if err := listPath(auth, token, childPath, prefix+"  "); err != nil {
				return err
			}
		} else {
			fmt.Printf("%s%s\n", prefix, name)
		}
	}
	return nil
}

func printHelp(authenticated bool) {

	fmt.Println("Commands:")

	fmt.Println("  help      Show available commands")

	fmt.Println("  exit      Close the client")

	if !authenticated {

		fmt.Println("  login     Log in")

		fmt.Println("  register  Create an account")

		return

	}

	fmt.Println("  logout    Log out and remove the saved session")

	fmt.Println("  ls [path] [-r] List directory contents; -r recursively")

	fmt.Println("  cd <path> Change remote directory")

	fmt.Println("  mkdir <path> Create a directory")

	fmt.Println("  rmdir [-f] [-r] <path> Remove directory; -r recursive, -f ignore missing path")

}
