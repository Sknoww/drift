package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Local-only changes (area 6). Like the rest of this package, these drive the
// real git binary: the whole design rests on git's own flags being the source of
// truth, so a test that mocked git would prove nothing about it.

// held is the two halves of the held set, joined the way the UI joins them.
func held(t *testing.T, dir string) (skipped, excluded []string) {
	t.Helper()
	r := New(dir)
	s, err := r.SkipWorktreeFiles(context.Background())
	if err != nil {
		t.Fatalf("SkipWorktreeFiles: %v", err)
	}
	e, err := r.ExcludedPaths(context.Background())
	if err != nil {
		t.Fatalf("ExcludedPaths: %v", err)
	}
	return s, e
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The headline promise for a tracked file: git stops reporting it as modified,
// so `add -A` and `commit -am` cannot sweep the edit into a commit.
func TestSkipWorktreeHidesATrackedEdit(t *testing.T) {
	dir := newRepo(t)
	commit(t, dir, "app.yml", "level: info\n")
	write(t, dir, "app.yml", "level: debug\n")

	r := New(dir)
	ctx := context.Background()
	if dirty, _ := r.IsDirty(ctx); !dirty {
		t.Fatal("precondition: the edit should show as dirty before the hold")
	}
	if err := r.SetSkipWorktree(ctx, "app.yml"); err != nil {
		t.Fatal(err)
	}

	if dirty, _ := r.IsDirty(ctx); dirty {
		t.Error("a held file still shows as dirty — git would still commit it")
	}
	skipped, _ := held(t, dir)
	if len(skipped) != 1 || skipped[0] != "app.yml" {
		t.Errorf("SkipWorktreeFiles() = %v, want [app.yml]", skipped)
	}

	// And the edit itself was never touched, which is what makes releasing safe.
	if got := readFile(t, filepath.Join(dir, "app.yml")); got != "level: debug\n" {
		t.Errorf("held file content = %q, want the local edit intact", got)
	}
}

// Releasing loses nothing: the edit reappears as an ordinary change, leaving the
// user to commit or discard it.
func TestClearSkipWorktreeBringsTheEditBack(t *testing.T) {
	dir := newRepo(t)
	commit(t, dir, "app.yml", "level: info\n")
	write(t, dir, "app.yml", "level: debug\n")

	r := New(dir)
	ctx := context.Background()
	if err := r.SetSkipWorktree(ctx, "app.yml"); err != nil {
		t.Fatal(err)
	}
	if err := r.ClearSkipWorktree(ctx, "app.yml"); err != nil {
		t.Fatal(err)
	}

	if dirty, _ := r.IsDirty(ctx); !dirty {
		t.Error("released file is not dirty — the local edit went missing")
	}
	if skipped, _ := held(t, dir); len(skipped) != 0 {
		t.Errorf("SkipWorktreeFiles() = %v, want empty after release", skipped)
	}
}

// update-index resolves its filenames against the *current* directory, so a
// Drift invoked from a subdirectory must still hold a repo-relative path — and
// ls-files, which otherwise lists only the directory it runs in, must still
// report the whole held set.
func TestHoldsAreRepoRelativeFromASubdirectory(t *testing.T) {
	dir := newRepo(t)
	commit(t, dir, "app.yml", "level: info\n")
	write(t, dir, "app.yml", "level: debug\n")

	sub := filepath.Join(dir, "deep", "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	r := New(sub)
	ctx := context.Background()
	if err := r.SetSkipWorktree(ctx, "app.yml"); err != nil {
		t.Fatalf("hold from a subdirectory: %v", err)
	}
	got, err := r.SkipWorktreeFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "app.yml" {
		t.Errorf("SkipWorktreeFiles() from a subdirectory = %v, want [app.yml]", got)
	}
}

// An untracked file is held by info/exclude instead, and git stops listing it.
func TestExcludeHidesAnUntrackedFile(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "docker-compose.override.yml", "ports: []\n")

	r := New(dir)
	ctx := context.Background()
	if err := r.AddExclude(ctx, "docker-compose.override.yml"); err != nil {
		t.Fatal(err)
	}

	changes, err := r.WorkingChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Errorf("WorkingChanges() = %v, want the excluded file gone from status", changes)
	}
	_, excluded := held(t, dir)
	if len(excluded) != 1 || excluded[0] != "docker-compose.override.yml" {
		t.Errorf("ExcludedPaths() = %v, want [docker-compose.override.yml]", excluded)
	}

	if err := r.RemoveExclude(ctx, "docker-compose.override.yml"); err != nil {
		t.Fatal(err)
	}
	if changes, _ = r.WorkingChanges(ctx); len(changes) != 1 {
		t.Errorf("WorkingChanges() after release = %v, want the file untracked again", changes)
	}
}

// Drift only ever edits its own fenced region — a hand-written exclude rule is
// the user's, and survives both a write and a full release.
func TestExcludeNeverClobbersHandWrittenRules(t *testing.T) {
	dir := newRepo(t)
	r := New(dir)
	ctx := context.Background()

	path, err := r.ExcludePath(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	const handWritten = "# mine\n*.log\n"
	if err := os.WriteFile(path, []byte(handWritten), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := r.AddExclude(ctx, "scratch.md"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); !strings.HasPrefix(got, handWritten) {
		t.Errorf("exclude file = %q, want the hand-written rules kept at the top", got)
	}
	// The hand-written *.log is not Drift's, so it is not reported as held.
	if _, excluded := held(t, dir); len(excluded) != 1 || excluded[0] != "scratch.md" {
		t.Errorf("ExcludedPaths() = %v, want only Drift's own entry", excluded)
	}

	if err := r.RemoveExclude(ctx, "scratch.md"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != handWritten {
		t.Errorf("exclude file after release = %q, want it back exactly as the user had it", got)
	}
}

// A second hold joins the block; holding the same path twice writes nothing.
func TestInsertExcludeJoinsTheBlockAndIsIdempotent(t *testing.T) {
	first, changed := insertExclude("", "a.txt")
	if !changed {
		t.Fatal("the first hold reported no change")
	}
	want := excludeHeader + "\n/a.txt\n" + fenceEnd + "\n"
	if first != want {
		t.Errorf("insertExclude() = %q, want %q", first, want)
	}

	second, changed := insertExclude(first, "b.txt")
	if !changed {
		t.Fatal("the second hold reported no change")
	}
	if want := excludeHeader + "\n/a.txt\n/b.txt\n" + fenceEnd + "\n"; second != want {
		t.Errorf("insertExclude() = %q, want the entry inside the block", second)
	}

	again, changed := insertExclude(second, "b.txt")
	if changed || again != second {
		t.Error("holding an already-held path wrote a duplicate")
	}
}

// Releasing the last held path takes the empty block with it, so the file goes
// back to how the user had it rather than keeping two markers around nothing.
func TestRemoveExcludeDropsAnEmptyBlock(t *testing.T) {
	content, _ := insertExclude("# mine\n*.log\n", "a.txt")
	got, changed := removeExclude(content, "a.txt")
	if !changed {
		t.Fatal("release reported no change")
	}
	if got != "# mine\n*.log\n" {
		t.Errorf("removeExclude() = %q, want the user's file back untouched", got)
	}

	if _, changed := removeExclude(got, "a.txt"); changed {
		t.Error("releasing an unheld path rewrote the file")
	}
}

// The leading slash is load-bearing: without it a gitignore pattern matches a
// basename at *any* depth, so holding one config.yml would quietly hold every
// config.yml in the tree.
func TestExcludePatternAnchorsToTheRepoRoot(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "config.yml", "root\n")
	write(t, dir, "service/config.yml", "nested\n")

	r := New(dir)
	ctx := context.Background()
	if err := r.AddExclude(ctx, "config.yml"); err != nil {
		t.Fatal(err)
	}

	changes, err := r.WorkingChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Path != "service/config.yml" {
		t.Errorf("WorkingChanges() = %v, want only the nested file still visible", changes)
	}
}

// A glob metacharacter in a filename is escaped, so the pattern holds that one
// file rather than everything it happens to match.
func TestExcludePatternEscapesGlobMetacharacters(t *testing.T) {
	if got, want := excludePattern("a[1]*.txt"), `/a\[1]\*.txt`; got != want {
		t.Errorf("excludePattern() = %q, want %q", got, want)
	}
	// And the round trip recovers exactly the path that went in — the reason the
	// exclude file can stay the single source of truth for untracked holds.
	for _, path := range []string{"a[1]*.txt", "plain.txt", "dir/sub/#odd!.md", "trailing "} {
		if got := excludedPath(excludePattern(path)); got != path {
			t.Errorf("round trip of %q = %q", path, got)
		}
	}
}

// The add flow's candidates: what git sees changed, tagged with the primitive
// that would hold each one.
func TestWorkingChangesRoutesByTracked(t *testing.T) {
	dir := newRepo(t)
	commit(t, dir, "app.yml", "level: info\n")
	write(t, dir, "app.yml", "level: debug\n")
	write(t, dir, "scratch.md", "notes\n")

	got, err := New(dir).WorkingChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byPath := make(map[string]WorkingChange, len(got))
	for _, c := range got {
		byPath[c.Path] = c
	}
	if c := byPath["app.yml"]; !c.Tracked || c.Staged {
		t.Errorf("app.yml = %+v, want tracked and unstaged", c)
	}
	if c := byPath["scratch.md"]; c.Tracked {
		t.Errorf("scratch.md = %+v, want untracked", c)
	}
}

// The one candidate a hold cannot honestly serve: skip-worktree hides the
// working tree, not the index, so a staged change would still be committed.
func TestWorkingChangesFlagsAStagedChange(t *testing.T) {
	dir := newRepo(t)
	commit(t, dir, "app.yml", "level: info\n")
	write(t, dir, "app.yml", "level: debug\n")
	git(t, dir, "add", "app.yml")

	got, err := New(dir).WorkingChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Staged {
		t.Errorf("WorkingChanges() = %+v, want the staged change flagged", got)
	}
}

// A rename spends a second NUL field on the original path; misreading it as a
// record of its own would invent a phantom candidate.
func TestWorkingChangesHandlesARename(t *testing.T) {
	dir := newRepo(t)
	commit(t, dir, "old.txt", "content\n")
	git(t, dir, "mv", "old.txt", "new.txt")
	write(t, dir, "scratch.md", "notes\n")

	got, err := New(dir).WorkingChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, c := range got {
		paths = append(paths, c.Path)
	}
	want := "new.txt,scratch.md"
	if strings.Join(paths, ",") != want {
		t.Errorf("WorkingChanges() paths = %v, want %v", paths, want)
	}
}

// No exclude file at all is an empty held set, not an error — a fresh clone has
// nothing held and must not read as broken.
func TestExcludedPathsWithNoFile(t *testing.T) {
	dir := newRepo(t)
	path, err := New(dir).ExcludePath(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	got, err := New(dir).ExcludedPaths(context.Background())
	if err != nil {
		t.Fatalf("ExcludedPaths() with no file: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ExcludedPaths() = %v, want empty", got)
	}
}

// A path git does not track cannot be held by skip-worktree — the error is
// correct, and is what routes such a path to info/exclude instead.
func TestSetSkipWorktreeRejectsAnUntrackedPath(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "scratch.md", "notes\n")

	if err := New(dir).SetSkipWorktree(context.Background(), "scratch.md"); err == nil {
		t.Error("SetSkipWorktree on an untracked path returned no error")
	}
}
