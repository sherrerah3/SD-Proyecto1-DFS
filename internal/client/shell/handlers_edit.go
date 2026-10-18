package shell

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"time"

	"dfsha/internal/client/api"
)

const (
	maxCatBytes      = 8 << 20 // largest file cat will print
	maxEditBytes     = 5 << 20 // largest file edit will open
	binaryProbeBytes = 1 << 20 // first block is enough to spot binary data
)

var errBinaryFile = errors.New("binary file; use 'get' to download it")

// remoteFiles is the part of the API client used by cat and edit.
type remoteFiles interface {
	Download(remotePath, localPath, token string, maxSize int64) (api.FileInfo, error)
	Put(localPath, remotePath, token string) error
	Replace(localPath, remotePath, baseSHA256, token string) error
}

var _ remoteFiles = (*api.Client)(nil)

// editorRunner opens file in the editor described by argv and waits for it.
type editorRunner func(argv []string, file string) error

func handleCat(s *shell, args []string) bool {
	if len(args) != 1 {
		fmt.Println("Usage: cat <file>")
		return true
	}

	remote := resolvePath(s.currentPath, args[0])
	if err := catFile(s.metanode, s.session.Token, remote, os.Stdout); err != nil {
		fmt.Printf("cat: %v\n", err)
	}

	return true
}

func handleEdit(s *shell, args []string) bool {
	return runEdit(s, "edit", args, "")
}

func handleNano(s *shell, args []string) bool {
	return runEdit(s, "nano", args, "nano")
}

func runEdit(s *shell, name string, args []string, forcedEditor string) bool {
	if len(args) != 1 {
		fmt.Printf("Usage: %s <file>\n", name)
		return true
	}

	argv, err := resolveEditor(forcedEditor, os.Getenv, exec.LookPath)
	if err != nil {
		fmt.Printf("%s: %v\n", name, err)
		return true
	}

	remote := resolvePath(s.currentPath, args[0])
	if err := editFile(s.metanode, s.session.Token, remote, argv, runEditorProcess, os.Stdout); err != nil {
		fmt.Printf("%s: %v\n", name, err)
	}

	return true
}

// catFile prints the file only after the download was verified.
func catFile(files remoteFiles, token, remote string, out io.Writer) error {
	dir, err := os.MkdirTemp("", "dfsha-cat-*")
	if err != nil {
		return fmt.Errorf("cannot create temporary directory: %w", err)
	}
	defer os.RemoveAll(dir)

	local := filepath.Join(dir, "content")
	if _, err := files.Download(remote, local, token, maxCatBytes); err != nil {
		return downloadError(err)
	}

	data, err := os.ReadFile(local)
	if err != nil {
		return fmt.Errorf("cannot read downloaded file: %w", err)
	}
	if looksBinary(data) {
		return errBinaryFile
	}

	clean, hidden := sanitizeForTerminal(data)
	if _, err := out.Write(clean); err != nil {
		return err
	}
	if len(clean) > 0 && clean[len(clean)-1] != '\n' {
		fmt.Fprintln(out)
	}
	if hidden > 0 {
		fmt.Fprintf(out, "[dfsha] %d control characters were hidden\n", hidden)
	}

	return nil
}

// editFile opens the file in the editor and uploads the result if it changed.
func editFile(
	files remoteFiles,
	token, remote string,
	editor []string,
	run editorRunner,
	out io.Writer,
) error {
	dir, err := os.MkdirTemp("", "dfsha-edit-*")
	if err != nil {
		return fmt.Errorf("cannot create temporary directory: %w", err)
	}
	defer os.RemoveAll(dir)

	name := safeFileName(path.Base(remote))
	local := filepath.Join(dir, name)

	base, isNew, err := fetchForEdit(files, token, remote, local, out)
	if err != nil {
		return err
	}

	before, err := hashFile(local)
	if err != nil {
		return err
	}

	if err := run(editor, local); err != nil {
		return fmt.Errorf("editor failed, nothing was uploaded: %w", err)
	}

	after, err := hashFile(local)
	if err != nil {
		return err
	}
	if after == before {
		fmt.Fprintln(out, "No changes, nothing uploaded.")
		return nil
	}

	if err := checkEditedSize(local); err != nil {
		return err
	}

	if err := upload(files, token, remote, local, base, isNew); err != nil {
		return uploadFailure(err, local, name)
	}

	fmt.Fprintf(out, "Saved %s\n", remote)
	return nil
}

// fetchForEdit downloads the file into local. A missing file starts empty.
func fetchForEdit(files remoteFiles, token, remote, local string, out io.Writer) (base api.FileInfo, isNew bool, err error) {
	base, err = files.Download(remote, local, token, maxEditBytes)
	switch {
	case err == nil:
		return base, false, rejectBinary(local)
	case isNotFound(err):
		fmt.Fprintf(out, "%s does not exist; it will be created if you save content\n", remote)
		if err := os.WriteFile(local, nil, 0o600); err != nil {
			return base, true, fmt.Errorf("cannot create temporary file: %w", err)
		}
		return base, true, nil
	default:
		return base, false, downloadError(err)
	}
}

// checkEditedSize rejects results that cannot be stored.
func checkEditedSize(local string) error {
	st, err := os.Stat(local)
	if err != nil {
		return err
	}
	if st.Size() == 0 {
		return errors.New("the file is empty and empty files cannot be stored; nothing was uploaded")
	}
	if st.Size() > maxEditBytes {
		return fmt.Errorf("the edited file is too large (%d bytes, limit %d)", st.Size(), maxEditBytes)
	}

	return nil
}

// upload creates the file, or replaces the version that was downloaded.
func upload(files remoteFiles, token, remote, local string, base api.FileInfo, isNew bool) error {
	if isNew {
		return files.Put(local, remote, token)
	}

	return files.Replace(local, remote, base.SHA256, token)
}

// uploadFailure keeps a copy of the user's work, since the temp dir is removed.
func uploadFailure(err error, local, name string) error {
	reason := fmt.Sprintf("upload failed: %v", err)
	var apiErr *api.Error
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict {
		reason = "the file changed on the server while you were editing"
	}

	saved, saveErr := saveRecovery(local, name)
	if saveErr != nil {
		return fmt.Errorf("%s; your edits could not be saved: %v", reason, saveErr)
	}

	return fmt.Errorf("%s; your version was saved to %s", reason, saved)
}

// saveRecovery copies the file to the current directory (mode 0600).
func saveRecovery(local, name string) (string, error) {
	data, err := os.ReadFile(local)
	if err != nil {
		return "", err
	}

	dest := name + ".dfsha-recovery"
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		dest = fmt.Sprintf("%s.dfsha-recovery-%d", name, time.Now().UnixNano())
		f, err = os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	}
	if err != nil {
		return "", err
	}

	if _, err := f.Write(data); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}

	return dest, nil
}

// downloadError suggests 'get' when a file is too large to show or edit.
func downloadError(err error) error {
	var tooLarge *api.TooLargeError
	if errors.As(err, &tooLarge) {
		return fmt.Errorf("file is too large (%d bytes, limit %d); use 'get' instead",
			tooLarge.Size, tooLarge.Max)
	}

	return err
}

// resolveEditor picks the editor command: forced, then $EDITOR, then nano and vi.
func resolveEditor(
	forced string,
	getenv func(string) string,
	lookPath func(string) (string, error),
) ([]string, error) {
	if forced != "" {
		if _, err := lookPath(forced); err != nil {
			return nil, fmt.Errorf("%s is not installed", forced)
		}
		return []string{forced}, nil
	}

	if argv := strings.Fields(getenv("EDITOR")); len(argv) > 0 {
		return argv, nil
	}

	for _, candidate := range []string{"nano", "vi"} {
		if _, err := lookPath(candidate); err == nil {
			return []string{candidate}, nil
		}
	}

	return nil, errors.New("no editor found; set the EDITOR environment variable")
}

// runEditorProcess runs the editor attached to the terminal until it exits.
func runEditorProcess(argv []string, file string) error {
	cmd := exec.Command(argv[0], append(append([]string{}, argv[1:]...), file)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Ctrl+C belongs to the editor, not to the shell.
	signal.Ignore(os.Interrupt)
	defer signal.Reset(os.Interrupt)

	return cmd.Run()
}

// looksBinary reports null bytes in the first block.
func looksBinary(data []byte) bool {
	if len(data) > binaryProbeBytes {
		data = data[:binaryProbeBytes]
	}
	return bytes.IndexByte(data, 0) >= 0
}

func rejectBinary(file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, binaryProbeBytes)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return err
	}
	if looksBinary(buf[:n]) {
		return errors.New("binary files cannot be edited; use 'get' to download it")
	}

	return nil
}

// sanitizeForTerminal drops control characters (they could rewrite the screen) and counts them.
func sanitizeForTerminal(data []byte) ([]byte, int) {
	clean := make([]byte, 0, len(data))
	hidden := 0

	for _, b := range data {
		switch {
		case b == '\n' || b == '\t':
			clean = append(clean, b)
		case b == '\r':
			// dropped silently: CRLF is common, a lone CR could overwrite the line
		case b < 0x20 || b == 0x7f:
			hidden++
		default:
			clean = append(clean, b)
		}
	}

	return clean, hidden
}

func hashFile(file string) (string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("cannot read temporary file: %w", err)
	}

	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// safeFileName keeps the extension (syntax highlighting) and avoids odd names.
func safeFileName(base string) string {
	base = filepath.Base(base)
	if base == "." || base == ".." || base == string(filepath.Separator) || base == "" {
		return "file"
	}

	return base
}
