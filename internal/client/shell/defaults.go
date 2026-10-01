package shell

const (
	shellVarProjectName = "dfsha"
	shellBannerText     = shellVarProjectName + " shell"

	shellDefaultPath = "/"

	sessionDirectoryName = ".dfsha"
	sessionFileName      = "session.json"

	shellLogDirectoryName = "logs/client"
	shellLogFileName      = "shell.log"

	commandHelp     = "help"
	commandClear    = "clear"
	commandLogin    = "login"
	commandRegister = "register"
	commandLogout   = "logout"
	commandLs       = "ls"
	commandCd       = "cd"
	commandPwd      = "pwd"
	commandMkdir    = "mkdir"
	commandRmdir    = "rmdir"
	commandRm       = "rm"
	commandPut      = "put"
	commandGet      = "get"
	commandOpen     = "open"
	commandClose    = "close"
	commandRead     = "read"
	commandWrite    = "write"
	commandLock     = "lock"
	commandExit     = "exit"

	commandExitAlias = "quit"

	terminalCtrlC = '\x03'
	terminalCtrlD = '\x04'
	terminalCtrlL = '\x0c'

	terminalBackspace = '\x08'
	terminalDelete    = '\x7f'

	terminalClearSequence = "\033[2J\033[H\n"
	terminalNewLine       = "\r\n"
	terminalCtrlCDisplay  = "^C\r\n"

	messageAvailableCommands = "Available commands:"
	messageNoActiveSession   = "Please log in or register to access authenticated commands."
	messageUseHelp           = "Use 'help' to see the available commands."
	messageLoggedInAs        = "Logged in as: %s"
	messageLoggedOut         = "Logged out."
	messageShellTerminated   = "shell terminated."

	errorUnknownCommand   = "unknown command: %s"
	errorAuthentication   = "authentication required. Use 'login' or 'register'."
	errorClearTerminal    = "failed to clear terminal: %v"
	errorCheckSession     = "failed to check session: %v"
	errorReadInput        = "failed to read input: %v"
	errorInitializeLogger = "failed to initialize shell logger: %v"
	errorCloseLogger      = "failed to close shell log file: %v"
	errorInputClosed      = "input closed"
	errorClearSession     = "failed to clear session: %v"
	errorNotImplemented   = "%s is not implemented yet."

	descriptionHelp     = "Show available commands"
	descriptionClear    = "Clear terminal (Ctrl+L)"
	descriptionLogin    = "Log in to " + shellVarProjectName
	descriptionRegister = "Register a new user in " + shellVarProjectName
	descriptionLogout   = "Log out from " + shellVarProjectName
	descriptionLs       = "List directory contents"
	descriptionCd       = "Change current directory"
	descriptionPwd      = "Print current directory"
	descriptionMkdir    = "Create a directory"
	descriptionRmdir    = "Remove a directory"
	descriptionRm       = "Remove a file"
	descriptionPut      = "Upload a file"
	descriptionGet      = "Download a file"
	descriptionOpen     = "Open a file"
	descriptionClose    = "Close a file"
	descriptionRead     = "Read from a file"
	descriptionWrite    = "Write to a file"
	descriptionLock     = "Lock a file"
	descriptionExit     = "Exit " + shellVarProjectName + " (Ctrl+C)"
)

var defaultCommands = []command{
	{
		name:         commandHelp,
		description:  descriptionHelp,
		requiresAuth: false,
		execute:      handleHelp,
	},
	{
		name:         commandClear,
		description:  descriptionClear,
		requiresAuth: false,
		execute:      handleClear,
	},
	{
		name:         commandLogin,
		description:  descriptionLogin,
		requiresAuth: false,
		execute:      handleLogin,
	},
	{
		name:         commandRegister,
		description:  descriptionRegister,
		requiresAuth: false,
		execute:      handleRegister,
	},
	{
		name:         commandLogout,
		description:  descriptionLogout,
		requiresAuth: true,
		execute:      handleLogout,
	},
	{
		name:         commandLs,
		description:  descriptionLs,
		requiresAuth: true,
		execute:      handleLs,
	},
	{
		name:         commandCd,
		description:  descriptionCd,
		requiresAuth: true,
		execute:      handleCd,
	},
	{
		name:         commandPwd,
		description:  descriptionPwd,
		requiresAuth: true,
		execute:      handlePwd,
	},
	{
		name:         commandMkdir,
		description:  descriptionMkdir,
		requiresAuth: true,
		execute:      handleMkdir,
	},
	{
		name:         commandRmdir,
		description:  descriptionRmdir,
		requiresAuth: true,
		execute:      handleRmdir,
	},
	{
		name:         commandRm,
		description:  descriptionRm,
		requiresAuth: true,
		execute:      handleRm,
	},
	{
		name:         commandPut,
		description:  descriptionPut,
		requiresAuth: true,
		execute:      handlePut,
	},
	{
		name:         commandGet,
		description:  descriptionGet,
		requiresAuth: true,
		execute:      handleGet,
	},
	{
		name:         commandOpen,
		description:  descriptionOpen,
		requiresAuth: true,
		execute:      handleOpen,
	},
	{
		name:         commandClose,
		description:  descriptionClose,
		requiresAuth: true,
		execute:      handleClose,
	},
	{
		name:         commandRead,
		description:  descriptionRead,
		requiresAuth: true,
		execute:      handleRead,
	},
	{
		name:         commandWrite,
		description:  descriptionWrite,
		requiresAuth: true,
		execute:      handleWrite,
	},
	{
		name:         commandLock,
		description:  descriptionLock,
		requiresAuth: true,
		execute:      handleLock,
	},
	{
		name:         commandExit,
		aliases:      []string{commandExitAlias},
		description:  descriptionExit,
		requiresAuth: false,
		execute:      handleExit,
	},
}
