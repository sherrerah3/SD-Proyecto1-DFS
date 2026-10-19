package shell

import (
	"dfsha/internal/client/api"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
)

func handleLs(s *shell, args []string) bool {
	recursive := false
	var target string

	for _, arg := range args {
		if arg == "-R" || arg == "-r" {
			recursive = true
			continue
		}

		if target != "" {
			fmt.Println("Usage: ls [-R] [directory]")
			return true
		}

		target = arg
	}

	dir := s.currentPath
	if target != "" {
		dir = resolvePath(s.currentPath, target)
	}

	if recursive {
		if err := listRecursive(s, dir); err != nil {
			fmt.Printf("ls: %v\n", err)
		}
		return true
	}

	entries, err := s.metanode.ListFiles(dir, s.session.Token)
	if err != nil {
		fmt.Printf("ls: %v\n", err)
		return true
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	for _, entry := range entries {
		if entry.Kind == "directory" {
			fmt.Println(entry.Name + "/")
		} else {
			fmt.Println(entry.Name)
		}
	}

	return true
}

// listRecursive muestra los archivos y directorios de una ruta
// y después recorre cada uno de sus subdirectorios.
func listRecursive(s *shell, dir string) error {
	entries, err := s.metanode.ListFiles(dir, s.session.Token)
	if err != nil {
		return err
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name == entries[j].Name {
			return entries[i].Kind < entries[j].Kind
		}
		return entries[i].Name < entries[j].Name
	})

	fmt.Printf("%s:\n", dir)

	for _, entry := range entries {
		if entry.Kind == "directory" {
			fmt.Println(entry.Name + "/")
		} else {
			fmt.Println(entry.Name)
		}
	}

	for _, entry := range entries {
		if entry.Kind != "directory" {
			continue
		}

		childPath := path.Join(dir, entry.Name)

		fmt.Println()

		if err := listRecursive(s, childPath); err != nil {
			return err
		}
	}

	return nil
}

func handleCd(s *shell, args []string) bool {
	if len(args) != 1 {
		fmt.Println("Usage: cd <directory>")
		return true
	}

	target := resolvePath(s.currentPath, args[0])

	if err := s.metanode.DirectoryExists(target, s.session.Token); err != nil {
		fmt.Printf("cd: %s: directory not found\n", target)
		return true
	}

	s.currentPath = target
	return true
}

func handlePwd(s *shell, args []string) bool {
	if len(args) != 0 {
		fmt.Println("Usage: pwd")
		return true
	}

	fmt.Println(s.currentPath)
	return true
}

func handleMkdir(s *shell, args []string) bool {
	if len(args) != 1 {
		fmt.Println("Usage: mkdir <directory>")
		return true
	}

	dir := resolvePath(s.currentPath, args[0])
	if dir == "/" {
		fmt.Println("mkdir: cannot recreate the root directory")
		return true
	}

	if err := s.metanode.CreateDirectory(dir, s.session.Token); err != nil {
		fmt.Printf("mkdir: %v\n", err)
		return true
	}

	return true
}

func handleRmdir(s *shell, args []string) bool {
	if len(args) != 1 {
		fmt.Println("Usage: rmdir <directory>")
		return true
	}

	dir := resolvePath(s.currentPath, args[0])
	if dir == "/" {
		fmt.Println("rmdir: cannot remove the root directory")
		return true
	}

	if dir == s.currentPath {
		fmt.Println("rmdir: cannot remove the current directory")
		return true
	}

	if err := s.metanode.RemoveDirectory(dir, s.session.Token); err != nil {
		fmt.Printf("rmdir: %v\n", err)
		return true
	}

	fmt.Printf("Directory removed: %s\n", dir)
	return true
}

func handleRm(s *shell, args []string) bool {
	recursive := false
	force := false
	var target string

	for _, arg := range args {
		if arg == "--" {
			continue
		}

		if strings.HasPrefix(arg, "-") && target == "" {
			for _, flag := range arg[1:] {
				switch flag {
				case 'r':
					recursive = true
				case 'f':
					force = true
				default:
					fmt.Printf("rm: invalid option -- '%c'\n", flag)
					fmt.Println("Usage: rm [-r] [-f] <path>")
					return true
				}
			}
			continue
		}

		if target != "" {
			fmt.Println("Usage: rm [-r] [-f] <path>")
			return true
		}

		target = arg
	}

	if target == "" {
		fmt.Println("Usage: rm [-r] [-f] <path>")
		return true
	}

	remotePath := resolvePath(s.currentPath, target)

	if remotePath == "/" {
		fmt.Println("rm: refusing to remove the root directory")
		return true
	}

	if remotePath == s.currentPath {
		fmt.Println("rm: refusing to remove the current directory")
		return true
	}

	isDirectory := s.metanode.DirectoryExists(
		remotePath,
		s.session.Token,
	) == nil

	if isDirectory && !recursive {
		fmt.Printf("rm: %s: is a directory (use -r)\n", remotePath)
		return true
	}

	var err error
	if isDirectory {
		err = removeRemoteDirectory(s, remotePath, force)
	} else {
		err = s.metanode.RemoveFile(remotePath, s.session.Token)
	}

	if err != nil {
		if force && isNotFound(err) {
			return true
		}
		fmt.Printf("rm: %v\n", err)
		return true
	}
	return true
}

// removeRemoteDirectory deletes children first and the directory last.

func removeRemoteDirectory(s *shell, dir string, force bool) error {
	entries, err := s.metanode.ListFiles(dir, s.session.Token)
	if err != nil {
		if force && isNotFound(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		child := path.Join(dir, entry.Name)

		if entry.Kind == "directory" {
			if err := removeRemoteDirectory(s, child, force); err != nil {
				return err
			}
			continue
		}

		if err := s.metanode.RemoveFile(child, s.session.Token); err != nil {
			if force && isNotFound(err) {
				continue
			}
			return err
		}

		fmt.Printf("Removed: %s\n", child)
	}

	if err := s.metanode.RemoveDirectory(dir, s.session.Token); err != nil {
		if force && isNotFound(err) {
			return nil
		}
		return err
	}

	fmt.Printf("Removed directory: %s\n", dir)
	return nil
}

// isNotFound identifies HTTP 404 errors returned by the metanode.
func isNotFound(err error) bool {
	var apiErr *api.Error
	return errors.As(err, &apiErr) && apiErr.Status == 404
}

func resolvePath(currentPath, target string) string {
	if path.IsAbs(target) {
		return path.Clean(target)
	}

	return path.Clean(path.Join(currentPath, target))
}
