package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests drive the real git binary against throwaway repos. Mocking git
// would only prove our parser matches our idea of git's output.

// TestMain drops git's system config, because driving the real binary means
// inheriting whatever that binary is configured to do — and that differs by
// machine. Apple ships a system gitconfig setting init.defaultBranch=main; CI's
// git has no such file. That one difference was enough to make seven tests pass
// on a Mac and fail on CI, for a reason that had nothing to do with the code
// under test. GIT_CONFIG_NOSYSTEM removes the file from git's search, so both
// see the same git.
//
// Only undeclared settings go away. Everything these tests actually rely on —
// user.name, user.email, the initial branch — is written per repo by the
// helpers below, which is where a test's dependencies belong.
func TestMain(m *testing.M) {
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	os.Exit(m.Run())
}

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

// commitAt commits with a fixed committer date, so a test can assert an order
// that depends on it. It shells out directly rather than through git(): the
// date is set by environment variable and run() owns the environment.
func commitAt(t *testing.T, dir, name, content, when string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", name)

	cmd := exec.Command("git", "commit", "--quiet", "-m", "add "+name)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_COMMITTER_DATE="+when, "GIT_AUTHOR_DATE="+when)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("setup: commit at %s: %v: %s", when, err, out)
	}
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

func TestRemoteBranches(t *testing.T) {
	// The wizard offers these as targets, so the real shape is what matters:
	// a clone with several branches pushed to origin, listed by their
	// origin/<name> short form — which is exactly a Target.Ref.
	//
	// Each branch gets its own committer date, and the dates run *against*
	// alphabetical order, so the recency sort cannot be mistaken for git's
	// default ordering having happened to agree.
	origin := newRepo(t)
	for _, b := range []struct{ name, when string }{
		{"aaa-stale", "2021-01-01T00:00:00Z"},
		{"release-perf", "2024-06-01T00:00:00Z"},
		{"zzz-fresh", "2026-05-01T00:00:00Z"},
	} {
		git(t, origin, "checkout", "--quiet", "-b", b.name, "main")
		commitAt(t, origin, b.name+".txt", "x", b.when)
	}
	git(t, origin, "checkout", "--quiet", "main")

	clone := t.TempDir()
	git(t, clone, "clone", "--quiet", origin, clone)
	r := New(clone)

	got, err := r.RemoteBranches(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Most recently updated first — the ordering that answers the wizard's
	// question, "which of these are your long-lived mains?" (roadmap area 14).
	// origin/HEAD is a symref to the default branch, not a branch to pick, so it
	// must be filtered out; origin/main carries the seed commit's date, which is
	// now and therefore sorts to the top.
	var refs []string
	for _, b := range got {
		refs = append(refs, b.Ref)
	}
	want := []string{"origin/main", "origin/zzz-fresh", "origin/release-perf", "origin/aaa-stale"}
	if strings.Join(refs, ",") != strings.Join(want, ",") {
		t.Fatalf("RemoteBranches() = %v, want %v", refs, want)
	}

	// The tip date comes back as a real time, not as git's English relative
	// form — the wizard formats its own age column from it.
	byRef := make(map[string]time.Time, len(got))
	for _, b := range got {
		if b.Updated.IsZero() {
			t.Errorf("%s came back with no tip date", b.Ref)
		}
		byRef[b.Ref] = b.Updated
	}
	if y := byRef["origin/aaa-stale"].UTC().Year(); y != 2021 {
		t.Errorf("origin/aaa-stale tip year = %d, want 2021", y)
	}
}

func TestRemoteBranchesNoRemote(t *testing.T) {
	// A repo with no remote has nothing to offer — an empty list, not an error.
	// main.go gates the wizard on this to fall back to the placeholder path.
	dir := newRepo(t)
	got, err := New(dir).RemoteBranches(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("RemoteBranches() with no remote = %v, want empty", got)
	}
}

func TestChangedFiles(t *testing.T) {
	dir := newRepo(t)
	r := New(dir)
	ctx := context.Background()

	// Diverge: the branch and main each touch their own file plus a shared one.
	git(t, dir, "checkout", "--quiet", "-b", "feature")
	commit(t, dir, "only-branch.txt", "b")
	commit(t, dir, "shared.txt", "branch version")
	git(t, dir, "checkout", "--quiet", "main")
	commit(t, dir, "only-main.txt", "m")
	commit(t, dir, "shared.txt", "main version")

	// Three-dot base...tip is what tip changed since the merge base, so the
	// direction of the pair decides whose changes you get.
	targetChanged, err := r.ChangedFiles(ctx, "feature", "main")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(targetChanged, ","), "only-main.txt,shared.txt"; got != want {
		t.Errorf("ChangedFiles(feature, main) = %q, want %q", got, want)
	}

	branchChanged, err := r.ChangedFiles(ctx, "main", "feature")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(branchChanged, ","), "only-branch.txt,shared.txt"; got != want {
		t.Errorf("ChangedFiles(main, feature) = %q, want %q", got, want)
	}
}

func TestFileDiff(t *testing.T) {
	dir := newRepo(t)
	r := New(dir)
	ctx := context.Background()

	git(t, dir, "checkout", "--quiet", "-b", "feature")
	git(t, dir, "checkout", "--quiet", "main")
	commit(t, dir, "shared.txt", "line one\nline two changed on main\n")
	commit(t, dir, "noise.txt", "unrelated")

	diff, err := r.FileDiff(ctx, "feature", "main", "shared.txt")
	if err != nil {
		t.Fatal(err)
	}
	// The diff is the incoming upstream change for exactly that path — the text
	// the panel shows in place of hunting for it.
	if !strings.Contains(diff, "line two changed on main") {
		t.Errorf("FileDiff() = %q, want it to contain the upstream change", diff)
	}
	if strings.Contains(diff, "noise.txt") {
		t.Errorf("FileDiff() leaked another path's change:\n%s", diff)
	}
}

func TestWorkingTreeModified(t *testing.T) {
	dir := newRepo(t)
	r := New(dir)
	ctx := context.Background()
	commit(t, dir, "tracked.txt", "original")

	if got, err := r.WorkingTreeModified(ctx); err != nil {
		t.Fatal(err)
	} else if len(got) != 0 {
		t.Errorf("WorkingTreeModified() on a clean tree = %v, want empty", got)
	}

	// An unstaged edit and a staged edit both count — a plain stash captures both.
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	commit(t, dir, "staged.txt", "seed")
	if err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "staged.txt")
	// An untracked file must NOT count: plain stash leaves it in place, so it
	// rides the shelve sequence through untouched.
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := r.WorkingTreeModified(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := "staged.txt,tracked.txt"; strings.Join(got, ",") != want {
		t.Errorf("WorkingTreeModified() = %v, want %q (untracked excluded)", got, want)
	}
}

func TestCheckAttrMerge(t *testing.T) {
	dir := newRepo(t)
	r := New(dir)
	ctx := context.Background()

	// The standard declaration: -merge (not the binary macro, which would also
	// kill -diff and so the panel).
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("*.uwe -merge\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := r.CheckAttrMerge(ctx, []string{"workflows/a.uwe", "src/main.go", "b.uwe"})
	if err != nil {
		t.Fatal(err)
	}
	if !got["workflows/a.uwe"] || !got["b.uwe"] {
		t.Errorf("CheckAttrMerge() = %v, want the .uwe paths flagged unmergeable", got)
	}
	if got["src/main.go"] {
		t.Errorf("CheckAttrMerge() flagged a mergeable path: %v", got)
	}
}

func TestCheckAttrMergeNoPaths(t *testing.T) {
	// No paths is a no-op, not a call that blocks reading check-attr's stdin.
	got, err := New(newRepo(t)).CheckAttrMerge(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("CheckAttrMerge(nil) = %v, want empty", got)
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
