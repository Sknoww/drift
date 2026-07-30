package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The shelve sequence's calls, driven against real repos. These are the calls
// that write to the working tree and to history, so a test that only proved our
// parser matches our idea of git's output would prove nothing worth knowing.

// cloneOf makes a clone of origin with a committer configured, the shape every
// shelve test needs: a branch that can fall behind an origin/<target> ref.
func cloneOf(t *testing.T, origin string) string {
	t.Helper()
	clone := t.TempDir()
	git(t, clone, "clone", "--quiet", origin, clone)
	git(t, clone, "config", "user.email", "test@example.com")
	git(t, clone, "config", "user.name", "Test")
	return clone
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRemoteRefSplitsOnTheRemoteNotTheFirstSlash(t *testing.T) {
	// The reason RemoteRef asks git for the remote list instead of cutting at the
	// first slash: branch names contain slashes routinely, and splitting the text
	// would turn "origin/release/2.1" into the branch "release" and fetch the
	// wrong thing — or nothing.
	origin := newRepo(t)
	git(t, origin, "checkout", "--quiet", "-b", "release/2.1")
	clone := cloneOf(t, origin)
	r := New(clone)
	ctx := context.Background()

	remote, branch, ok, err := r.RemoteRef(ctx, "origin/release/2.1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || remote != "origin" || branch != "release/2.1" {
		t.Errorf("RemoteRef() = (%q, %q, %v), want (origin, release/2.1, true)", remote, branch, ok)
	}
}

func TestRemoteRefRejectsARefNoRemoteOwns(t *testing.T) {
	// A target that is not remote-tracking has nothing to pull. Reporting that is
	// the point: the sequence says so and merges the ref as it stands, rather than
	// silently pretending it fetched.
	r := New(newRepo(t))

	_, _, ok, err := r.RemoteRef(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("RemoteRef(\"main\") reported a remote for a purely local ref")
	}
}

func TestFetchRefUpdatesOnlyItsOwnTarget(t *testing.T) {
	// Scoping the pull to the one target being merged is what keeps a sequence
	// started for one branch from quietly moving every other branch's numbers.
	origin := newRepo(t)
	git(t, origin, "checkout", "--quiet", "-b", "other")
	git(t, origin, "checkout", "--quiet", "main")
	clone := cloneOf(t, origin)
	r := New(clone)
	ctx := context.Background()

	otherBefore := strings.TrimSpace(git(t, clone, "rev-parse", "origin/other"))

	// Both branches move on the server.
	commit(t, origin, "on-main.txt", "main")
	git(t, origin, "checkout", "--quiet", "other")
	commit(t, origin, "on-other.txt", "other")

	if err := r.FetchRef(ctx, "origin", "main"); err != nil {
		t.Fatal(err)
	}

	ab, err := r.AheadBehind(ctx, "main", "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	if ab.Behind != 1 {
		t.Errorf("after FetchRef(main), behind = %d, want 1 — the target's ref did not move", ab.Behind)
	}
	if got := strings.TrimSpace(git(t, clone, "rev-parse", "origin/other")); got != otherBefore {
		t.Error("FetchRef(main) also moved origin/other; the pull is meant to be scoped to one target")
	}
}

func TestStashLeavesLocalOnlyChangesInPlace(t *testing.T) {
	// The headline promise of area 6, and the one thing the shelve step owes it:
	// a plain stash cannot see a skip-worktree file or an untracked one, so both
	// ride the sequence through with no re-apply step.
	dir := newRepo(t)
	commit(t, dir, "app.conf", "level=info\n")
	r := New(dir)
	ctx := context.Background()

	// A tracked file held with skip-worktree, edited locally.
	write(t, dir, "app.conf", "level=debug\n")
	if err := r.SetSkipWorktree(ctx, "app.conf"); err != nil {
		t.Fatal(err)
	}
	// An untracked scratch file.
	write(t, dir, "scratch.txt", "notes\n")
	// And ordinary work, which is what the stash is actually for.
	write(t, dir, "seed.txt", "real work\n")

	oid, err := r.Stash(ctx, "drift: shelve test")
	if err != nil {
		t.Fatal(err)
	}
	if oid == "" {
		t.Fatal("Stash() reported nothing stashed, but seed.txt was modified")
	}

	if got := read(t, dir, "app.conf"); got != "level=debug\n" {
		t.Errorf("held tracked file after stash = %q, want the local edit untouched", got)
	}
	if got := read(t, dir, "scratch.txt"); got != "notes\n" {
		t.Errorf("untracked file after stash = %q, want it untouched", got)
	}
	if got := read(t, dir, "seed.txt"); got != "seed" {
		t.Errorf("ordinary work after stash = %q, want it reverted into the stash", got)
	}

	if conflicts, err := r.StashPop(ctx, oid); err != nil || len(conflicts) != 0 {
		t.Fatalf("StashPop() = %v, %v; want a clean pop", conflicts, err)
	}
	if got := read(t, dir, "app.conf"); got != "level=debug\n" {
		t.Errorf("held tracked file after pop = %q, want the local edit still in place", got)
	}
	if got := read(t, dir, "seed.txt"); got != "real work\n" {
		t.Errorf("ordinary work after pop = %q, want it restored", got)
	}
}

func TestStashReportsNothingStashedOnACleanTree(t *testing.T) {
	// The footgun this signature exists to close: `git stash push` on a clean tree
	// *succeeds* and creates no entry. A caller that popped unconditionally
	// afterwards would pop whatever unrelated work was on the stack.
	dir := newRepo(t)
	r := New(dir)
	ctx := context.Background()

	// Someone else's stash, already on the stack.
	write(t, dir, "seed.txt", "theirs")
	git(t, dir, "stash", "push", "-m", "not drift's")
	theirs, err := r.StashRef(ctx)
	if err != nil {
		t.Fatal(err)
	}

	oid, err := r.Stash(ctx, "drift: shelve test")
	if err != nil {
		t.Fatal(err)
	}
	if oid != "" {
		t.Fatalf("Stash() on a clean tree = %q, want \"\" — nothing was stashed", oid)
	}
	if top, _ := r.StashRef(ctx); top != theirs {
		t.Error("Stash() on a clean tree disturbed the existing stack")
	}
}

func TestStashRefIsEmptyWithNoStash(t *testing.T) {
	got, err := New(newRepo(t)).StashRef(context.Background())
	if err != nil {
		t.Fatalf("StashRef() with no refs/stash errored: %v — absent is the answer, not a failure", err)
	}
	if got != "" {
		t.Errorf("StashRef() = %q, want \"\"", got)
	}
}

func TestStashPopRefusesWhenTheStashMoved(t *testing.T) {
	// `stash@{0}` is a position, not an identity. Anything that stashes
	// concurrently shifts it, and popping the wrong stash is not the kind of git
	// mistake you can walk back.
	dir := newRepo(t)
	r := New(dir)
	ctx := context.Background()

	write(t, dir, "seed.txt", "drift's work")
	ours, err := r.Stash(ctx, "drift: shelve test")
	if err != nil {
		t.Fatal(err)
	}

	// Something else stashes while the sequence is mid-flight.
	write(t, dir, "seed.txt", "someone else's work")
	git(t, dir, "stash", "push", "-m", "another terminal")

	conflicts, err := r.StashPop(ctx, ours)
	if !errors.Is(err, ErrStashMoved) {
		t.Fatalf("StashPop() = %v, %v; want ErrStashMoved", conflicts, err)
	}
	if got := strings.Count(git(t, dir, "stash", "list"), "\n"); got != 2 {
		t.Errorf("stash list has %d entries after the refusal, want both still there", got)
	}
}

func TestStashPopPreservesTheStagedSplit(t *testing.T) {
	// --index is why: with something staged, a plain pop would flatten the
	// staged/unstaged split the user built into one unstaged pile.
	dir := newRepo(t)
	commit(t, dir, "staged.txt", "before")
	commit(t, dir, "unstaged.txt", "before")
	r := New(dir)
	ctx := context.Background()

	write(t, dir, "staged.txt", "after")
	git(t, dir, "add", "staged.txt")
	write(t, dir, "unstaged.txt", "after")

	oid, err := r.Stash(ctx, "drift: shelve test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.StashPop(ctx, oid); err != nil {
		t.Fatal(err)
	}

	if got := lines(git(t, dir, "diff", "--cached", "--name-only")); len(got) != 1 || got[0] != "staged.txt" {
		t.Errorf("staged paths after pop = %v, want [staged.txt] — the split was flattened", got)
	}
}

func TestMergeCleanAndConflicted(t *testing.T) {
	dir := newRepo(t)
	commit(t, dir, "shared.txt", "base\n")
	r := New(dir)
	ctx := context.Background()

	git(t, dir, "checkout", "--quiet", "-b", "feature")
	commit(t, dir, "mine.txt", "mine\n")
	git(t, dir, "checkout", "--quiet", "main")
	commit(t, dir, "theirs.txt", "theirs\n")

	git(t, dir, "checkout", "--quiet", "feature")
	conflicts, err := r.Merge(ctx, "main")
	if err != nil {
		t.Fatalf("Merge() on disjoint changes errored: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("Merge() = %v, want a clean merge", conflicts)
	}

	// Now both sides commit to the same file — the case the sequence stops on.
	git(t, dir, "checkout", "--quiet", "main")
	write(t, dir, "shared.txt", "theirs\n")
	git(t, dir, "commit", "--quiet", "-am", "theirs")
	git(t, dir, "checkout", "--quiet", "feature")
	write(t, dir, "shared.txt", "mine\n")
	git(t, dir, "commit", "--quiet", "-am", "mine")

	conflicts, err = r.Merge(ctx, "main")
	if err != nil {
		t.Fatalf("Merge() reported a conflict as an error: %v — a conflict is a result", err)
	}
	if len(conflicts) != 1 || conflicts[0] != "shared.txt" {
		t.Fatalf("Merge() conflicts = %v, want [shared.txt]", conflicts)
	}

	if op, err := r.OperationInProgress(ctx); err != nil || op != "a merge" {
		t.Errorf("OperationInProgress() = %q, %v; want \"a merge\"", op, err)
	}

	if err := r.MergeAbort(ctx); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, "shared.txt"); got != "mine\n" {
		t.Errorf("after MergeAbort, shared.txt = %q, want the branch's own version back", got)
	}
	if op, err := r.OperationInProgress(ctx); err != nil || op != "" {
		t.Errorf("OperationInProgress() after abort = %q, %v; want idle", op, err)
	}
	if got, err := r.ConflictedFiles(ctx); err != nil || len(got) != 0 {
		t.Errorf("ConflictedFiles() after abort = %v, %v; want none", got, err)
	}
}

func TestMergeFFCatchesUpButNeverMergesABranchWithItself(t *testing.T) {
	// Roadmap 19c. The step exists so `u` is not wrong on a second machine — a
	// stale branch has to catch up — and it may do no more than that: a merge here
	// is the branch merged with itself, which nobody asked for.
	dir := newRepo(t)
	commit(t, dir, "shared.txt", "base\n")
	r := New(dir)
	ctx := context.Background()

	git(t, dir, "checkout", "--quiet", "-b", "upstream")
	commit(t, dir, "theirs.txt", "from my other machine\n")
	git(t, dir, "checkout", "--quiet", "main")

	// Stale but not diverged: the whole case the step is kept for.
	diverged, err := r.MergeFF(ctx, "upstream")
	if err != nil {
		t.Fatalf("MergeFF() on a stale branch errored: %v — it has to catch up", err)
	}
	if diverged {
		t.Fatal("MergeFF() called a plain fast-forward a divergence")
	}
	if got := read(t, dir, "theirs.txt"); got != "from my other machine\n" {
		t.Errorf("theirs.txt = %q, want the branch fast-forwarded", got)
	}

	// Already level. Nothing to do is not a divergence either.
	if diverged, err := r.MergeFF(ctx, "upstream"); err != nil || diverged {
		t.Errorf("MergeFF() on an already-level branch = %v, %v; want no-op, not a divergence", diverged, err)
	}

	// Now both sides have moved, so no fast-forward exists.
	git(t, dir, "checkout", "--quiet", "upstream")
	commit(t, dir, "theirs2.txt", "more of theirs\n")
	git(t, dir, "checkout", "--quiet", "main")
	commit(t, dir, "mine.txt", "mine\n")

	diverged, err = r.MergeFF(ctx, "upstream")
	if err != nil {
		t.Fatalf("MergeFF() reported a divergence as an error: %v — it is a result", err)
	}
	if !diverged {
		t.Fatal("MergeFF() fast-forwarded a diverged branch, or called the refusal something else")
	}
	// The refusal touches nothing, which is what lets the halt above it be a plain
	// handoff rather than a rollback: git declines before it writes.
	if op, err := r.OperationInProgress(ctx); err != nil || op != "" {
		t.Errorf("OperationInProgress() = %q, %v; want idle — --ff-only must not leave a merge in flight", op, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "theirs2.txt")); !os.IsNotExist(err) {
		t.Error("the refused fast-forward brought content in anyway")
	}
	if ab, err := r.AheadBehind(ctx, "main", "upstream"); err != nil || ab.Ahead != 1 || ab.Behind != 1 {
		t.Errorf("main vs upstream = %+v, %v; want the divergence left exactly as it was", ab, err)
	}
}

func TestMergeFFReportsARealFailureAsAnError(t *testing.T) {
	// The other half of the probe: a --ff-only that fails for any reason *other*
	// than a missing fast-forward must not be dressed up as a divergence, or the
	// halt would name a reconciliation for a problem the user does not have.
	dir := newRepo(t)
	commit(t, dir, "shared.txt", "base\n")
	r := New(dir)

	diverged, err := r.MergeFF(context.Background(), "refs/heads/no-such-branch")
	if err == nil {
		t.Fatal("MergeFF() on a ref that does not resolve returned no error")
	}
	if diverged {
		t.Error("MergeFF() called an unresolvable ref a divergence")
	}
}

func TestOperationInProgressIdle(t *testing.T) {
	op, err := New(newRepo(t)).OperationInProgress(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if op != "" {
		t.Errorf("OperationInProgress() on an idle repo = %q, want \"\"", op)
	}
}

func TestStashPopReportsAConflictRatherThanFailing(t *testing.T) {
	// The destination, not a failure: the target's merged version and the user's
	// uncommitted work both changed the file. git retains the stash entry when a
	// pop conflicts, which is what makes halting in place safe.
	dir := newRepo(t)
	commit(t, dir, "shared.txt", "base\n")
	r := New(dir)
	ctx := context.Background()

	git(t, dir, "checkout", "--quiet", "-b", "feature")
	write(t, dir, "shared.txt", "my uncommitted work\n")

	oid, err := r.Stash(ctx, "drift: shelve test")
	if err != nil {
		t.Fatal(err)
	}
	// The target's version lands underneath while the work is stashed.
	write(t, dir, "shared.txt", "the target's version\n")
	git(t, dir, "commit", "--quiet", "-am", "incoming")

	conflicts, err := r.StashPop(ctx, oid)
	if err != nil {
		t.Fatalf("StashPop() reported a conflict as an error: %v", err)
	}
	if len(conflicts) != 1 || conflicts[0] != "shared.txt" {
		t.Fatalf("StashPop() conflicts = %v, want [shared.txt]", conflicts)
	}
	if got := git(t, dir, "stash", "list"); !strings.Contains(got, "drift: shelve test") {
		t.Error("the stash entry was dropped on a conflicted pop; the user's work must stay retained")
	}
}
