package shell

const (
	promptUsernameLabel = "Username: "
	promptPasswordLabel = "Password: "
	promptConfirmLabel  = "Confirm password: "

	messageRegistered = "User %s registered."

	errorAlreadyLoggedIn  = "already logged in as %s. Use 'logout' first."
	errorEmptyUsername    = "username cannot be empty."
	errorEmptyPassword    = "password cannot be empty."
	errorPasswordMismatch = "passwords do not match."
	errorLogin            = "login failed: %v"
	errorRegister         = "registration failed: %v"
	errorSaveSession      = "logged in, but the session could not be saved: %v"
)
