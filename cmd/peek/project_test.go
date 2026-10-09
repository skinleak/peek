package main

import (
	"os"
	"path/filepath"
	"testing"
)

// realDir returns a temporary directory with symlinks resolved, since on
// macOS the temporary directory lives behind one.
func realDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestProjectRoot(t *testing.T) {
	home := realDir(t)
	repo := filepath.Join(home, "code", "webapp")
	mkdir(t, filepath.Join(repo, ".git"))
	mkdir(t, filepath.Join(repo, "apps", "api"))
	worktree := filepath.Join(home, "code", "webapp-feature")
	mkdir(t, filepath.Join(worktree, "src"))
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: ../webapp/.git/worktrees/feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(home, "scratch")
	mkdir(t, scratch)
	link := filepath.Join(home, "link")
	if err := os.Symlink(filepath.Join(repo, "apps"), link); err != nil {
		t.Fatal(err)
	}
	// A dotfiles repository in the home directory isn't a project.
	mkdir(t, filepath.Join(home, ".git"))

	tests := []struct{ arg, want string }{
		{repo, repo},
		{filepath.Join(repo, "apps", "api"), repo},
		{filepath.Join(worktree, "src"), worktree},
		{scratch, scratch},
		{"~/scratch", scratch},
		{link, repo},
		{home, home},
	}
	for _, tt := range tests {
		got, err := projectRoot(tt.arg, home)
		if err != nil {
			t.Errorf("projectRoot(%q): %v", tt.arg, err)
			continue
		}
		if got != tt.want {
			t.Errorf("projectRoot(%q) = %q, want %q", tt.arg, got, tt.want)
		}
	}

	file := filepath.Join(scratch, "notes.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := projectRoot(file, home); err == nil {
		t.Errorf("projectRoot(%q): want an error for a file", file)
	}
}

func TestIsDirArg(t *testing.T) {
	for _, s := range []string{".", "..", "~", "~/code", "./api", "/srv/app", "apps/api"} {
		if !isDirArg(s) {
			t.Errorf("isDirArg(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"3000", "3000-3999", "kill", "300o", "webapp"} {
		if isDirArg(s) {
			t.Errorf("isDirArg(%q) = true, want false", s)
		}
	}
}
