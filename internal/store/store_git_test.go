package store

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sknoww/drift/internal/git"
)

// The rest of the package's tests fake GitDir. These drive the real git layer,
// because "which directory does Drift write to" is the one thing a fake cannot
// answer honestly.

func initRepo(t *testing.T) string {
	t.Helper()
	// git reports real paths, and on macOS t.TempDir() hands back one behind
	// the /var -> /private/var symlink. Resolve up front so the test compares
	// like with like.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "--quiet", "--initial-branch=main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"commit", "--quiet", "--allow-empty", "-m", "seed"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup: git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	return dir
}

func TestResolveUsesGitDir(t *testing.T) {
	dir := initRepo(t)

	paths, err := Resolve(context.Background(), git.New(dir))
	if err != nil {
		t.Fatal(err)
	}

	// Inside .git is what makes Drift's files per-repo and unversioned for free.
	want := filepath.Join(dir, ".git", "drift")
	if paths.Dir != want {
		t.Errorf("Resolve().Dir = %q, want %q", paths.Dir, want)
	}
	if paths.Config != filepath.Join(want, "config.json") {
		t.Errorf("Resolve().Config = %q", paths.Config)
	}
	if paths.State != filepath.Join(want, "state.json") {
		t.Errorf("Resolve().State = %q", paths.State)
	}
}

func TestResolveFromSubdirectory(t *testing.T) {
	// Drift is invoked from wherever the user is standing inside the repo, so
	// every invocation must land on the same files.
	dir := initRepo(t)
	sub := filepath.Join(dir, "src", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	fromRoot, err := Resolve(ctx, git.New(dir))
	if err != nil {
		t.Fatal(err)
	}
	fromSub, err := Resolve(ctx, git.New(sub))
	if err != nil {
		t.Fatal(err)
	}
	if fromRoot != fromSub {
		t.Errorf("Resolve() from a subdirectory = %+v, want %+v", fromSub, fromRoot)
	}
}

func TestDriftFilesAreNotVersioned(t *testing.T) {
	// The whole reason for living inside .git: nothing Drift writes may ever
	// show up as a change to the user's repository.
	dir := initRepo(t)
	ctx := context.Background()
	repo := git.New(dir)

	if _, _, err := LoadConfig(ctx, repo); err == nil {
		t.Fatal("expected a placeholder error on first run")
	}
	if err := SaveState(ctx, repo, Store{Tickets: []Ticket{{ID: "ABC-123"}}}); err != nil {
		t.Fatal(err)
	}

	dirty, err := repo.IsDirty(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if dirty {
		cmd := exec.Command("git", "status", "--porcelain")
		cmd.Dir = dir
		out, _ := cmd.CombinedOutput()
		t.Errorf("writing drift's files dirtied the repo:\n%s", out)
	}
}
