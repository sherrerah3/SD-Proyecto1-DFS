package shell

import (
	"fmt"
	"path"
	"sort"
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
	fmt.Printf(errorNotImplemented, commandRmdir)
	fmt.Println()
	return true
}

func handleRm(s *shell, args []string) bool {
	fmt.Printf(errorNotImplemented, commandRm)
	fmt.Println()
	return true
}

func resolvePath(currentPath, target string) string {
	if path.IsAbs(target) {
		return path.Clean(target)
	}

	return path.Clean(path.Join(currentPath, target))
}
