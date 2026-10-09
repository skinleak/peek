package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// isDirArg reports whether a positional argument names a directory rather
// than a port: ".", "..", anything with a slash, or "~". Ports never look
// like that, so a typo such as "300o" stays a port error instead of becoming
// a confusing "no such directory".
func isDirArg(s string) bool {
	return s == "." || s == ".." || s == "~" || strings.ContainsRune(s, '/')
}

// projectRoot resolves a directory argument to the project it belongs to:
// the enclosing git repository, or the directory itself outside one. Symlinks
// are resolved, as the kernel reports working directories without them.
func projectRoot(arg, home string) (string, error) {
	if home != "" && (arg == "~" || strings.HasPrefix(arg, "~/")) {
		arg = home + arg[1:]
	}
	abs, err := filepath.Abs(arg)
	if err != nil {
		return "", err
	}
	dir, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("no such directory %q", arg)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("%q is not a directory", arg)
	}
	if h, err := filepath.EvalSymlinks(home); err == nil {
		home = h
	}
	if root := gitRoot(dir, home); root != "" {
		return root, nil
	}
	return dir, nil
}

// gitRoot returns the closest directory at or above dir that contains .git
// (a directory, or a file for worktrees and submodules), or "" if there is
// none. A repository in the home directory or at / doesn't count: it's
// usually dotfiles, and taking it would make every directory one project.
func gitRoot(dir, home string) string {
	for {
		if dir == home || dir == filepath.Dir(dir) {
			return ""
		}
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
}
