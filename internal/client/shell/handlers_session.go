package shell

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// handleLogin: login [usuario]. Pide la contraseña sin mostrarla.
func handleLogin(s *shell, args []string) bool {
	if rejectIfLoggedIn(s) {
		return true
	}

	username, password, ok := readCredentials(s, args, false)
	if !ok {
		fmt.Println()

		return true
	}

	logIn(s, username, password)

	fmt.Println()

	return true
}

// handleRegister: register [usuario]. Crea el usuario y abre sesión.
func handleRegister(s *shell, args []string) bool {
	if rejectIfLoggedIn(s) {
		return true
	}

	username, password, ok := readCredentials(s, args, true)
	if !ok {
		fmt.Println()

		return true
	}

	if err := s.metanode.Register(username, password); err != nil {
		reportShellError(
			s,
			fmt.Sprintf(
				errorRegister,
				err,
			),
		)

		fmt.Println()

		return true
	}

	fmt.Printf(
		messageRegistered,
		username,
	)

	fmt.Println()

	logIn(s, username, password)

	fmt.Println()

	return true
}

func handleLogout(s *shell, args []string) bool {
	if err := clearSession(); err != nil {
		reportShellError(
			s,
			fmt.Sprintf(
				errorClearSession,
				err,
			),
		)

		fmt.Println()

		return true
	}

	s.session = nil

	fmt.Println(messageLoggedOut)

	return true
}

// logIn pide el token al metanode y guarda la sesión en memoria y en disco.
func logIn(s *shell, username, password string) {
	result, err := s.metanode.Login(username, password)
	if err != nil {
		reportShellError(
			s,
			fmt.Sprintf(
				errorLogin,
				err,
			),
		)

		return
	}

	s.session = &session{
		Username: username,
		Token:    result.Token,
	}

	if err := saveSession(s.session); err != nil {
		reportShellError(
			s,
			fmt.Sprintf(
				errorSaveSession,
				err,
			),
		)
	}

	fmt.Printf(
		messageLoggedInAs,
		username,
	)
}

func rejectIfLoggedIn(s *shell) bool {
	if s.session == nil {
		return false
	}

	reportShellError(
		s,
		fmt.Sprintf(
			errorAlreadyLoggedIn,
			s.session.Username,
		),
	)
	return true
}

// readCredentials pide usuario y contraseña (dos veces si confirm es true).
// Si algo falla, ya reporta el error y devuelve ok=false.
func readCredentials(s *shell, args []string, confirm bool) (string, string, bool) {
	username, err := askUsername(s, args)
	if err != nil {
		reportInputError(s, err)

		return "", "", false
	}

	if username == "" {
		reportShellError(s, errorEmptyUsername)

		return "", "", false
	}

	password, err := promptPassword(s, promptPasswordLabel)
	if err != nil {
		reportInputError(s, err)

		return "", "", false
	}

	if password == "" {
		reportShellError(s, errorEmptyPassword)

		return "", "", false
	}

	if confirm {
		again, err := promptPassword(s, promptConfirmLabel)
		if err != nil {
			reportInputError(s, err)

			return "", "", false
		}

		if again != password {
			reportShellError(s, errorPasswordMismatch)

			return "", "", false
		}
	}

	return username, password, true
}

func reportInputError(s *shell, err error) {
	reportShellError(
		s,
		fmt.Sprintf(
			errorReadInput,
			err,
		),
	)
}

// askUsername usa el primer argumento si existe; si no, lo pregunta.
func askUsername(s *shell, args []string) (string, error) {
	if len(args) > 0 {
		return strings.TrimSpace(args[0]), nil
	}

	fmt.Print(promptUsernameLabel)

	line, err := s.reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(line), nil
}

// promptPassword lee la contraseña sin eco. Si la entrada no es una terminal
// (por ejemplo, un pipe), lee una línea normal.
func promptPassword(s *shell, label string) (string, error) {
	fmt.Print(label)

	fileDescriptor := int(os.Stdin.Fd())

	if !term.IsTerminal(fileDescriptor) {
		line, err := s.reader.ReadString('\n')
		if err != nil {
			return "", err
		}

		return strings.TrimRight(line, "\r\n"), nil
	}

	raw, err := term.ReadPassword(fileDescriptor)

	fmt.Println()

	if err != nil {
		return "", err
	}

	return string(raw), nil
}
