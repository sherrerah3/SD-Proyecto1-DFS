package shell

import (
	"fmt"
	"path"
)

func handlePut(s *shell, args []string) bool {
	if len(args) < 1 || len(args) > 2 {
		fmt.Println("Usage: put <local-file> [remote-path]")
		return true
	}

	localPath := args[0]
	var remotePath string

	if len(args) == 2 {
		remotePath = args[1]
	} else {
		remotePath = path.Join(s.currentPath, path.Base(localPath))
	}

	if !path.IsAbs(remotePath) {
		remotePath = path.Join(s.currentPath, remotePath)
	}

	if err := s.metanode.Put(
		localPath,
		remotePath,
		s.session.Token,
	); err != nil {
		fmt.Printf("Failed to upload file: %v\n", err)
		return true
	}

	fmt.Printf("File uploaded successfully: %s -> %s\n", localPath, remotePath)
	return true
}

func handleGet(s *shell, args []string) bool {
	fmt.Printf(
		errorNotImplemented,
		commandGet,
	)

	fmt.Println()

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
