package shell

import (
	"fmt"
	"os"
	"path"
	"strings"
)

func parseVerboseArgs(command string, args []string, usage string) (bool, []string, bool) {
	verbose := false
	positional := make([]string, 0, len(args))
	optionsEnded := false

	for _, arg := range args {
		if !optionsEnded && arg == "--" {
			optionsEnded = true
			continue
		}
		if !optionsEnded && (arg == "-v" || arg == "--verbose") {
			verbose = true
			continue
		}
		if !optionsEnded && strings.HasPrefix(arg, "-") {
			fmt.Printf("%s: unknown option: %s\n", command, arg)
			fmt.Printf("Usage: %s\n", usage)
			return false, nil, false
		}
		positional = append(positional, arg)
	}

	return verbose, positional, true
}

func handlePut(s *shell, args []string) bool {
	verbose, positional, valid := parseVerboseArgs(
		"put", args, "put [-v | --verbose] [--] <local-file> [remote-path]",
	)
	if !valid {
		return true
	}
	if len(positional) < 1 || len(positional) > 2 {
		fmt.Println("Usage: put [-v | --verbose] [--] <local-file> [remote-path]")
		return true
	}

	localPath := positional[0]
	var remotePath string
	if len(positional) == 2 {
		remotePath = positional[1]
	} else {
		remotePath = path.Join(s.currentPath, path.Base(localPath))
	}
	if !path.IsAbs(remotePath) {
		remotePath = path.Join(s.currentPath, remotePath)
	}

	var err error
	if verbose {
		fmt.Printf("[put] archivo local=%s remoto=%s\n", localPath, remotePath)
		err = s.metanode.PutVerbose(localPath, remotePath, s.session.Token, os.Stdout)
	} else {
		err = s.metanode.Put(localPath, remotePath, s.session.Token)
	}
	if err != nil {
		fmt.Printf("Failed to upload file: %v\n", err)
		return true
	}

	fmt.Printf("File uploaded successfully: %s -> %s\n", localPath, remotePath)
	return true
}

func handleGet(s *shell, args []string) bool {
	verbose, positional, valid := parseVerboseArgs(
		"get", args, "get [-v | --verbose] [--] <remote-path> [local-path]",
	)
	if !valid {
		return true
	}
	if len(positional) < 1 || len(positional) > 2 {
		fmt.Println("Usage: get [-v | --verbose] [--] <remote-path> [local-path]")
		return true
	}

	remotePath := resolvePath(s.currentPath, positional[0])
	localPath := ""
	if len(positional) == 2 {
		localPath = positional[1]
	} else {
		localPath = path.Base(remotePath)
	}

	var err error
	if verbose {
		fmt.Printf("[get] archivo remoto=%s local=%s\n", remotePath, localPath)
		err = s.metanode.GetVerbose(remotePath, localPath, s.session.Token, os.Stdout)
	} else {
		err = s.metanode.Get(remotePath, localPath, s.session.Token)
	}
	if err != nil {
		fmt.Printf("Failed to download file: %v\n", err)
		return true
	}

	fmt.Printf("File downloaded successfully: %s -> %s\n", remotePath, localPath)
	return true
}

func handleOpen(s *shell, args []string) bool {
	fmt.Printf(
		errorNotImplemented,
		commandOpen,
	)

	fmt.Println()

	return true
}

func handleClose(s *shell, args []string) bool {
	fmt.Printf(
		errorNotImplemented,
		commandClose,
	)

	fmt.Println()

	return true
}

func handleRead(s *shell, args []string) bool {
	fmt.Printf(
		errorNotImplemented,
		commandRead,
	)

	fmt.Println()

	return true
}

func handleWrite(s *shell, args []string) bool {
	fmt.Printf(
		errorNotImplemented,
		commandWrite,
	)

	fmt.Println()

	return true
}
