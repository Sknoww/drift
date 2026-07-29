package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The update sequence's three new calls, driven against real repos. Push in
// particular is the reason: its interesting outcome is a *rejection*, which git
// reports as a non-zero exit with the detail on stdout, and a test against a
// fixture string would only prove the parser matches our idea of that.

// bareOrigin makes a bare repo with one commit on main and returns its path,
// the shape a clone can actually push back to. A non-bare origin refuses a push
// to its checked-out branch, which would make every push test fail for a reason
// that has nothing to do with what is being tested.
//
// --initial-branch is not optional: it sets the bare repo's HEAD, and a HEAD
// naming a branch we never push leaves `git clone` with nothing to check out.
// Without it the name is whatever init.defaultBranch happens to say. TestMain
// takes the system config out of that answer, but a global config still has a
// vote, so the branch is named here rather than assumed.
func bareOrigin(t *testing.T) string {
	t.Helper()
	work := newRepo(t)
	bare := t.TempDir()
	git(t, work, "init", "--quiet", "--bare", "--initial-branch=main", bare)
	git(t, work, "push", "--quiet", bare, "main")
	return bare
}

func TestCheckoutSwitchesBranches(t *testing.T) {
	dir := newRepo(t)
	git(t, dir, "branch", "feature")
	r := New(dir)
	ctx := context.Background()

	if err := r.Checkout(ctx, "feature"); err != nil {
		t.Fatal(err)
	}
	got, err := r.CurrentBranch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "feature" {
		t.Errorf("CurrentBranch() = %q after Checkout(feature)", got)
	}
}

func TestCheckoutRefusesToOverwriteUncommittedWork(t *testing.T) {
	// The refusal is a result the sequence needs, not an obstacle: `u` stashes
	// before it switches, so a checkout that still cannot proceed means something
	// the stash could not see is in the way — and writing over that is exactly
	// what area 6 exists to prevent.
	dir := newRepo(t)
	git(t, dir, "checkout", "--quiet", "-b", "feature")
	commit(t, dir, "shared.txt", "from feature")
	git(t, dir, "checkout", "--quiet", "main")
	if err := os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("uncommitted"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := New(dir)

	if err := r.Checkout(context.Background(), "feature"); err == nil {
		t.Fatal("Checkout overwrote an uncommitted file instead of refusing")
	}
}

func TestUpstreamNamesTheTrackedRef(t *testing.T) {
	origin := newRepo(t)
	clone := cloneOf(t, origin)
	r := New(clone)

	got, err := r.Upstream(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if got != "origin/main" {
		t.Errorf("Upstream(main) = %q, want origin/main", got)
	}
}

func TestUpstreamIsEmptyForAnUnpublishedBranch(t *testing.T) {
	// Not an error: a branch that has never been pushed has nothing to pull and
	// nowhere to publish, and that is the answer the sequence acts on.
	origin := newRepo(t)
	clone := cloneOf(t, origin)
	git(t, clone, "branch", "local-only")
	r := New(clone)

	got, err := r.Upstream(context.Background(), "local-only")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("Upstream(local-only) = %q, want \"\"", got)
	}
}

func TestUpstreamIsEmptyForABranchThatIsGone(t *testing.T) {
	// A pairing can outlive the branch it names. Reporting "no upstream" rather
	// than failing keeps that a halt the sequence explains, not a git error
	// handed to the user raw.
	clone := cloneOf(t, newRepo(t))
	r := New(clone)

	got, err := r.Upstream(context.Background(), "never-existed")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("Upstream(never-existed) = %q, want \"\"", got)
	}
}

func TestUpstreamsAnswersForEveryLocalBranchAtOnce(t *testing.T) {
	// The dashboard's sweep asks this once for the whole repo rather than once per
	// row. The distinction it depends on is present-with-an-empty-value against
	// absent: the first is a branch that has never been published, the second is a
	// branch that is not here at all, and the row says different things about them.
	origin := newRepo(t)
	clone := cloneOf(t, origin)
	git(t, clone, "branch", "local-only")
	r := New(clone)

	got, err := r.Upstreams(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got["main"] != "origin/main" {
		t.Errorf("Upstreams()[main] = %q, want origin/main", got["main"])
	}
	up, present := got["local-only"]
	if !present {
		t.Error("an unpublished branch is missing from the map; it is local, it just tracks nothing")
	}
	if up != "" {
		t.Errorf("Upstreams()[local-only] = %q, want \"\"", up)
	}
	if _, present := got["never-existed"]; present {
		t.Error("a branch that does not exist locally must be absent, not empty")
	}
}

func TestPushPublishesTheBranch(t *testing.T) {
	origin := bareOrigin(t)
	clone := cloneOf(t, origin)
	commit(t, clone, "mine.txt", "mine")
	r := New(clone)
	ctx := context.Background()

	got, err := r.Push(ctx, "origin", "main", "main")
	if err != nil {
		t.Fatal(err)
	}
	if got != PushUpdated {
		t.Errorf("Push() = %v, want PushUpdated", got)
	}
	ab, err := r.AheadBehind(ctx, "main", "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	if ab.Ahead != 0 {
		t.Errorf("main is still %d ahead of origin/main after a push", ab.Ahead)
	}
}

func TestPushReportsNothingToSend(t *testing.T) {
	// "Published" and "already published" are different claims about the branch,
	// and the report says which — so a `u` that pushed nothing does not imply it
	// did, and one that did is not written off as a no-op.
	origin := bareOrigin(t)
	clone := cloneOf(t, origin)
	r := New(clone)

	got, err := r.Push(context.Background(), "origin", "main", "main")
	if err != nil {
		t.Fatal(err)
	}
	if got != PushUpToDate {
		t.Errorf("Push() = %v on an unchanged branch, want PushUpToDate", got)
	}
}

func TestPushReportsARejectionRatherThanFailing(t *testing.T) {
	// The case the area exists to hand back: the branch moved on the remote after
	// we last read it, so the push is not a fast-forward. Someone else's commit is
	// in the way — never a force, and never mistaken for an unreachable remote.
	origin := bareOrigin(t)
	mine := cloneOf(t, origin) // cloned before their push, so it predates it
	other := cloneOf(t, origin)
	commit(t, other, "theirs.txt", "theirs")
	git(t, other, "push", "--quiet", "origin", "main")

	commit(t, mine, "mine.txt", "mine")
	r := New(mine)

	got, err := r.Push(context.Background(), "origin", "main", "main")
	if err != nil {
		t.Fatalf("a rejection came back as an error rather than an outcome: %v", err)
	}
	if got != PushRejected {
		t.Errorf("Push() = %v against a moved remote, want PushRejected", got)
	}
}

func TestPushFailsLoudlyWhenTheRemoteIsUnreachable(t *testing.T) {
	// The other half of the rejection test: a non-zero exit with no per-ref status
	// is a real failure, and must not be quietly read as "rejected" — the two need
	// opposite responses.
	clone := cloneOf(t, bareOrigin(t))
	git(t, clone, "remote", "add", "gone", filepath.Join(t.TempDir(), "nowhere"))
	r := New(clone)

	if _, err := r.Push(context.Background(), "gone", "main", "main"); err == nil {
		t.Error("Push to an unreachable remote reported success")
	}
}

func TestPushHonorsAnUpstreamUnderAnotherName(t *testing.T) {
	// The refspec is written out rather than left to push.default: a branch may
	// track an upstream carrying a different name, and publishing the right
	// commits to the wrong ref is the failure that would hide behind a bare push.
	origin := bareOrigin(t)
	clone := cloneOf(t, origin)
	git(t, clone, "checkout", "--quiet", "-b", "local-name")
	commit(t, clone, "mine.txt", "mine")
	r := New(clone)
	ctx := context.Background()

	if _, err := r.Push(ctx, "origin", "local-name", "remote-name"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.run(ctx, "fetch", "--quiet", "origin"); err != nil {
		t.Fatal(err)
	}
	ab, err := r.AheadBehind(ctx, "local-name", "origin/remote-name")
	if err != nil {
		t.Fatalf("origin/remote-name was never created: %v", err)
	}
	if ab.Ahead != 0 || ab.Behind != 0 {
		t.Errorf("local-name vs origin/remote-name = %+v, want level", ab)
	}
}
