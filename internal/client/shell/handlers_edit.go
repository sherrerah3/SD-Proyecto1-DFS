package shell

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"dfsha/internal/client/api"
)

const (
	editHeartbeatInterval = 10 * time.Second
)

func handleEdit(s *shell, args []string) bool {
	if len(args) != 1 {
		fmt.Println("Usage: edit <remote-path>")
		return true
	}

	remotePath := resolvePath(s.currentPath, args[0])

	// Acquire the exclusive editing lease.
	lock, err := s.metanode.Lock(remotePath, s.session.Token)
	if err != nil {
		fmt.Printf("edit: cannot acquire file lock: %v\n", err)
		return true
	}

	// Best-effort unlock on every exit path.
	defer func() {
		if err := s.metanode.Unlock(remotePath, s.session.Token, lock); err != nil {
			fmt.Printf("edit: warning: could not release lock: %v\n", err)
		}
	}()

	// Download the file to a local temporary copy.
	localFile, err := os.CreateTemp(
		"",
		"dfsha-edit-*"+filepath.Ext(remotePath),
	)
	if err != nil {
		fmt.Printf("edit: cannot create local copy: %v\n", err)
		return true
	}

	localPath := localFile.Name()
	if err := localFile.Close(); err != nil {
		fmt.Printf("edit: cannot close temporary file: %v\n", err)
		return true
	}

	if err := s.metanode.Get(remotePath, localPath, s.session.Token); err != nil {
		_ = os.Remove(localPath)
		fmt.Printf("edit: download failed: %v\n", err)
		return true
	}

	// Renew the lease while the editor is open and during upload.
	var leaseLost atomic.Bool
	stopHeartbeat := make(chan struct{})
	heartbeatDone := make(chan struct{})

	go func() {
		defer close(heartbeatDone)

		ticker := time.NewTicker(editHeartbeatInterval)
		defer ticker.Stop()

		for {
			select {
			case <-stopHeartbeat:
				return

			case <-ticker.C:
				renewed, err := s.metanode.Heartbeat(
					remotePath,
					s.session.Token,
					lock,
				)
				if err != nil {
					leaseLost.Store(true)
					fmt.Printf(
						"\nedit: heartbeat failed; saving may be unsafe: %v\n",
						err,
					)
					return
				}
				lock = renewed
			}
		}
	}()

	stopRenewal := func() {
		select {
		case <-stopHeartbeat:
		default:
			close(stopHeartbeat)
		}
		<-heartbeatDone
	}

	// Open the editor and wait for it to exit.
	fmt.Printf("Editing %s\n", remotePath)

	if err := openLocalEditor(localPath); err != nil {
		stopRenewal()
		fmt.Printf("edit: editor failed: %v\n", err)
		fmt.Printf("Local copy preserved at: %s\n", localPath)
		return true
	}

	// Do not upload if the lease was lost.
	if leaseLost.Load() {
		stopRenewal()
		fmt.Printf("edit: lock lost; refusing to upload.\n")
		fmt.Printf("Local copy preserved at: %s\n", localPath)
		return true
	}

	// The lock-aware upload must validate the lease on the server.
	if err := s.metanode.PutWithLock(
		localPath,
		remotePath,
		s.session.Token,
		lock,
	); err != nil {
		stopRenewal()
		fmt.Printf("edit: upload failed: %v\n", err)
		fmt.Printf("Local copy preserved at: %s\n", localPath)
		return true
	}

	stopRenewal()

	// Remove the local copy only after a successful upload.
	if err := os.Remove(localPath); err != nil {
		fmt.Printf("edit: saved remotely, but could not remove %s: %v\n",
			localPath, err)
		return true
	}

	fmt.Println("File updated successfully.")
	return true
}

func openLocalEditor(filePath string) error {
	if runtime.GOOS == "windows" {
		// /WAIT ensures the shell waits for Notepad to close.
		cmd := exec.Command(
			"cmd.exe",
			"/C",
			"start",
			"",
			"/WAIT",
			"notepad.exe",
			filePath,
		)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// VISUAL is preferred for full-screen interactive editors. EDITOR is
	// commonly set on Linux and macOS. Treat either value as an executable
	// path/name (not a shell command).
	editor := strings.TrimSpace(os.Getenv("VISUAL"))
	if editor == "" {
		editor = strings.TrimSpace(os.Getenv("EDITOR"))
	}

	if editor == "" {
		// Choose an editor that is actually installed, rather than assuming
		// that every Linux image contains vi.
		for _, candidate := range []string{"nano", "vim", "vi", "editor"} {
			if _, err := exec.LookPath(candidate); err == nil {
				editor = candidate
				break
			}
		}
	}
	if editor == "" {
		return fmt.Errorf("no terminal editor found; install nano, vim, or vi, or set EDITOR")
	}

	cmd := exec.Command(editor, filePath)
	// Editors such as vi and nano require the client's terminal. Without
	// forwarding these streams, they receive /dev/null and typically exit
	// immediately with status 1 (especially inside Docker containers).
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Keep path imported for remote-path handling in this file if needed
// by future editor-related helpers.
var _ = path.Clean

// Keep api referenced for documentation and editor API extensions.
var _ api.FileLock
