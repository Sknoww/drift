package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests drive the real git binary against throwaway repos. Mocking git
// would only prove our parser matches our idea of git's output.

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	r := New(dir)
	out, err := r.run(context.Background(), args...)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	return out
}

// newRepo makes an empty repo with one commit on branch "main".
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "--quiet", "--initial-branch=main")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "Test")
	commit(t, dir, "seed.txt", "seed")
	return dir
}

func commit(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", name)
	git(t, dir, "commit", "--quiet", "-m", "add "+name)
}

func TestLocalBranches(t *testing.T) {
	dir := newRepo(t)
	git(t, dir, "branch", "ABC-123-feature")
	git(t, dir, "branch", "other")

	got, err := New(dir).LocalBranches(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ABC-123-feature", "main", "other"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("LocalBranches() = %v, want %v", got, want)
	}
}

func TestCurrentBranch(t *testing.T) {
	dir := newRepo(t)
	got, err := New(dir).CurrentBranch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "main" {
		t.Errorf("CurrentBranch() = %q, want %q", got, "main")
	}
}

func TestCurrentBranchDetached(t *testing.T) {
	dir := newRepo(t)
	head := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
	git(t, dir, "checkout", "--quiet", head)

	got, err := New(dir).CurrentBranch(context.Background())
	if err != nil {
		t.Fatalf("detached HEAD must not error: %v", err)
	}
	if got != "" {
		t.Errorf("CurrentBranch() on detached HEAD = %q, want \"\"", got)
	}
}

func TestIsDirty(t *testing.T) {
	dir := newRepo(t)
	r := New(dir)

	dirty, err := r.IsDirty(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if dirty {
		t.Error("IsDirty() = true on a clean tree, want false")
	}

	// An untracked file counts: --porcelain lists it, and it can block a merge.
	if err := os.WriteFile(filepath.Join(dir, "scratch.txt"), []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirty, err = r.IsDirty(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !dirty {
		t.Error("IsDirty() = false with an untracked file, want true")
	}
}

func TestAheadBehind(t *testing.T) {
	dir := newRepo(t)
	r := New(dir)
	ctx := context.Background()

	// Branch off, then advance both sides so the counts differ from each other
	// and a swapped left/right would be visible.
	git(t, dir, "checkout", "--quiet", "-b", "feature")
	commit(t, dir, "f1.txt", "1")
	commit(t, dir, "f2.txt", "2")
	git(t, dir, "checkout", "--quiet", "main")
	commit(t, dir, "m1.txt", "1")
	git(t, dir, "checkout", "--quiet", "feature")

	got, err := r.AheadBehind(ctx, "feature", "main")
	if err != nil {
		t.Fatal(err)
	}
	want := AheadBehind{Ahead: 2, Behind: 1}
	if got != want {
		t.Errorf("AheadBehind(feature, main) = %+v, want %+v", got, want)
	}
}

func TestAheadBehindIdentical(t *testing.T) {
	dir := newRepo(t)
	git(t, dir, "branch", "copy")

	got, err := New(dir).AheadBehind(context.Background(), "copy", "main")
	if err != nil {
		t.Fatal(err)
	}
	if (got != AheadBehind{}) {
		t.Errorf("AheadBehind() on identical branches = %+v, want zero", got)
	}
}

func TestAheadBehindUnknownRef(t *testing.T) {
	dir := newRepo(t)
	_, err := New(dir).AheadBehind(context.Background(), "main", "origin/nope")
	if err == nil {
		t.Fatal("AheadBehind() with an unknown target ref = nil error, want an error")
	}
	if !strings.Contains(err.Error(), "origin/nope") {
		t.Errorf("error %q should name the bad ref", err)
	}
}

func TestCandidateBranches(t *testing.T) {
	dir := newRepo(t)
	for _, b := range []string{
		"ABC-123-fix-login",
		"feature/abc-123/r2perf",
		"hotfix-ABC-1234",
		"unrelated",
	} {
		git(t, dir, "branch", b)
	}
	r := New(dir)

	tests := []struct {
		name   string
		ticket string
		want   []string
	}{
		{
			name:   "case-insensitive across naming styles",
			ticket: "ABC-123",
			// ABC-1234 contains "ABC-123" as a substring — substring matching is
			// the documented rule, and the user confirms every match anyway.
			want: []string{"ABC-123-fix-login", "feature/abc-123/r2perf", "hotfix-ABC-1234"},
		},
		{
			name:   "lowercase ticket matches uppercase branch",
			ticket: "abc-1234",
			want:   []string{"hotfix-ABC-1234"},
		},
		{
			name:   "no match",
			ticket: "XYZ-9",
			want:   nil,
		},
		{
			name:   "empty ticket matches nothing, not everything",
			ticket: "",
			want:   nil,
		},
		{
			name:   "whitespace-only ticket matches nothing",
			ticket: "   ",
			want:   nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.CandidateBranches(context.Background(), tt.ticket)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("CandidateBranches(%q) = %v, want %v", tt.ticket, got, tt.want)
			}
		})
	}
}

func TestFetchAndAheadBehindAgainstRemote(t *testing.T) {
	// The real shape: a branch compared to origin/<target> after the target
	// moved on the server. This is the signal the dashboard exists to show.
	origin := newRepo(t)
	ctx := context.Background()

	clone := t.TempDir()
	git(t, clone, "clone", "--quiet", origin, clone)
	git(t, clone, "config", "user.email", "test@example.com")
	git(t, clone, "config", "user.name", "Test")
	r := New(clone)

	git(t, clone, "checkout", "--quiet", "-b", "feature")
	commit(t, clone, "mine.txt", "mine")

	before, err := r.AheadBehind(ctx, "feature", "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	if want := (AheadBehind{Ahead: 1, Behind: 0}); before != want {
		t.Fatalf("AheadBehind() before the target moved = %+v, want %+v", before, want)
	}

	// main moves on the server; without a fetch we cannot see it.
	commit(t, origin, "theirs.txt", "theirs")

	stale, err := r.AheadBehind(ctx, "feature", "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	if stale != before {
		t.Errorf("AheadBehind() without a fetch = %+v, want it unchanged at %+v", stale, before)
	}

	if err := r.Fetch(ctx); err != nil {
		t.Fatal(err)
	}

	after, err := r.AheadBehind(ctx, "feature", "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	if want := (AheadBehind{Ahead: 1, Behind: 1}); after != want {
		t.Errorf("AheadBehind() after fetch = %+v, want %+v", after, want)
	}
}

func TestGitDir(t *testing.T) {
	dir := newRepo(t)

	got, err := New(dir).GitDir(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("GitDir() = %q, want an absolute path", got)
	}
	if filepath.Base(got) != ".git" {
		t.Errorf("GitDir() = %q, want it to end in .git", got)
	}
	if _, err := os.Stat(got); err != nil {
		t.Errorf("GitDir() = %q, which does not exist: %v", got, err)
	}
}

func TestGitDirFromSubdirectory(t *testing.T) {
	// Drift is invoked from wherever the user is standing, not the repo root.
	dir := newRepo(t)
	sub := filepath.Join(dir, "nested", "deeper")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	want, err := New(dir).GitDir(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, err := New(sub).GitDir(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("GitDir() from a subdirectory = %q, want %q", got, want)
	}
}

func TestGitDirInWorktree(t *testing.T) {
	// The case that justifies asking git instead of joining <root>/.git: in a
	// linked worktree .git is a file, and the real git dir lives elsewhere.
	dir := newRepo(t)
	wt := filepath.Join(t.TempDir(), "linked")
	git(t, dir, "worktree", "add", "--quiet", "-b", "wt-branch", wt)

	got, err := New(wt).GitDir(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got == filepath.Join(wt, ".git") {
		t.Errorf("GitDir() in a worktree = %q, but .git there is a file, not the git dir", got)
	}
	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("GitDir() = %q, which does not exist: %v", got, err)
	}
	if !info.IsDir() {
		t.Errorf("GitDir() = %q, want a directory", got)
	}
}

func TestErrorsOutsideARepo(t *testing.T) {
	dir := t.TempDir() // not a repo
	r := New(dir)
	ctx := context.Background()

	if _, err := r.LocalBranches(ctx); err == nil {
		t.Error("LocalBranches() outside a repo = nil error, want an error")
	}
	if _, err := r.IsDirty(ctx); err == nil {
		t.Error("IsDirty() outside a repo = nil error, want an error")
	}
}

func TestContextCancellation(t *testing.T) {
	dir := newRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := New(dir).LocalBranches(ctx); err == nil {
		t.Error("LocalBranches() with a cancelled context = nil error, want an error")
	}
}
