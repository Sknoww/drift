package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// The shelve sequence's git calls (roadmap area 7, docs/specs/shelve-sequence.md):
// stash → pull the target → merge → pop, plus the probes that decide whether the
// sequence may start and the checks that decide where it stops.
//
// This is the first place Drift writes to the working tree and to history. Two
// rules shape what is here. Nothing parses git's English — an outcome is read
// from refs, exit status, and the unmerged index, all of which are stable across
// versions and locales. And every mutating call is paired with the probe that
// tells the caller what actually happened, so the UI never has to assume the
// effect of its own write.

// ErrStashMoved reports that the top of the stash stack is no longer the entry
// Drift created, so popping would restore someone else's work onto a branch it
// was never taken from. `stash@{0}` is a position, not an identity: anything that
// stashes concurrently — another terminal, an IDE, a hook — shifts it.
var ErrStashMoved = errors.New("the stash Drift created is no longer on top of the stack")

// Remotes lists the repo's configured remotes.
func (r *Repo) Remotes(ctx context.Context) ([]string, error) {
	out, err := r.run(ctx, "remote")
	if err != nil {
		return nil, err
	}
	return lines(out), nil
}

// RemoteRef splits a remote-tracking ref such as "origin/release/2.1" into the
// remote that owns it and the branch on that remote, reporting ok=false for a ref
// no configured remote claims.
//
// The remote list is asked of git rather than assumed from the text: branch names
// contain slashes routinely, so splitting on the first one would turn
// "origin/release/2.1" into the branch "release" and fetch the wrong thing (or
// nothing). The longest matching remote wins, so a remote whose name prefixes
// another's can't shadow it.
func (r *Repo) RemoteRef(ctx context.Context, ref string) (remote, branch string, ok bool, err error) {
	remotes, err := r.Remotes(ctx)
	if err != nil {
		return "", "", false, err
	}
	for _, rem := range remotes {
		rest, found := strings.CutPrefix(ref, rem+"/")
		if !found || rest == "" {
			continue
		}
		if len(rem) > len(remote) {
			remote, branch, ok = rem, rest, true
		}
	}
	return remote, branch, ok, nil
}

// FetchRef updates one remote-tracking ref — the pull half of the shelve
// sequence, which merges `origin/<target>` without ever checking it out.
//
// Scoped to the single target being merged rather than reusing Fetch's whole-
// remote sweep: it is faster, and it cannot quietly move every *other* branch's
// ahead/behind numbers partway through a sequence the user started for one of
// them. Git updates the corresponding refs/remotes ref opportunistically, which
// is exactly what `git pull` relies on.
func (r *Repo) FetchRef(ctx context.Context, remote, branch string) error {
	_, err := r.run(ctx, "fetch", "--quiet", remote, branch)
	return err
}

// OperationInProgress names the git operation the repo is already in the middle
// of — "a merge", "a rebase" — and returns "" when it is idle. Drift refuses to
// start a shelve sequence on top of one: the repo is not in the state the
// sequence assumes, and stacking on it turns one problem into two.
//
// Read as file existence under the git dir, which is where git itself keeps these
// markers, and per-worktree because GitDir resolves the worktree's own git dir.
func (r *Repo) OperationInProgress(ctx context.Context) (string, error) {
	dir, err := r.GitDir(ctx)
	if err != nil {
		return "", err
	}
	probes := []struct{ file, name string }{
		{"MERGE_HEAD", "a merge"},
		{"CHERRY_PICK_HEAD", "a cherry-pick"},
		{"REVERT_HEAD", "a revert"},
		{"rebase-merge", "a rebase"},
		{"rebase-apply", "a rebase"},
	}
	for _, p := range probes {
		switch _, err := os.Stat(filepath.Join(dir, p.file)); {
		case err == nil:
			return p.name, nil
		case !errors.Is(err, os.ErrNotExist):
			return "", err
		}
	}
	return "", nil
}

// ConflictedFiles lists the paths git has left unmerged — the index's own record
// of a stopped merge or a stopped stash pop. It is how Merge and StashPop tell a
// conflict from a failure without reading git's message text.
func (r *Repo) ConflictedFiles(ctx context.Context) ([]string, error) {
	out, err := r.run(ctx, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	return lines(out), nil
}

// StashRef resolves the commit the top of the stash stack points at, returning ""
// when the repo has no stash at all. Absent is not an error — a repo that has
// never stashed has no refs/stash, which is the common case.
//
// for-each-ref rather than `rev-parse --verify`: a missing ref makes rev-parse
// exit non-zero, which would have to be told apart from a real failure by
// sniffing an exit code, while for-each-ref answers "no such ref" as empty output
// and exit 0. Same reason the branch listings upstream in git.go use it.
func (r *Repo) StashRef(ctx context.Context) (string, error) {
	out, err := r.run(ctx, "for-each-ref", "--format=%(objectname)", "refs/stash")
	if err != nil {
		return "", err
	}
	got := lines(out)
	if len(got) == 0 {
		return "", nil
	}
	return got[0], nil
}

// Stash shelves the working tree and returns the commit of the entry it created,
// or "" when there was nothing to stash.
//
// That second case is the trap this signature exists to close: `git stash push`
// with a clean tree *succeeds*, printing "No local changes to save" and creating
// no entry. A caller that popped unconditionally afterwards would pop whatever
// unrelated work happened to be on the stack. Drift resolves refs/stash before
// and after and reports what actually happened, rather than assuming its own
// write had an effect.
//
// Deliberately a plain push — no -u, no -a. Untracked files and skip-worktree
// files are what area 6 holds on this machine, and a plain stash cannot see
// either, which is how they ride the whole sequence through with no re-apply
// step (docs/specs/local-only-changes.md).
func (r *Repo) Stash(ctx context.Context, message string) (string, error) {
	before, err := r.StashRef(ctx)
	if err != nil {
		return "", err
	}
	if _, err := r.run(ctx, "stash", "push", "-m", message); err != nil {
		return "", err
	}
	after, err := r.StashRef(ctx)
	if err != nil {
		return "", err
	}
	if after == "" || after == before {
		return "", nil // clean tree: nothing was stashed, so there is nothing to pop
	}
	return after, nil
}

// StashPop restores the entry identified by oid, which must still be the top of
// the stack — see ErrStashMoved. It returns the conflicting paths when the pop
// stops on a conflict, which is not an error: the entry is *retained* by git in
// that case, so the user's work is still safely stashed and the caller's job is
// to say so rather than to recover.
//
// --index is used unconditionally. With nothing staged it behaves exactly like a
// plain pop; with something staged it is what preserves the staged/unstaged split
// the user built, which a plain pop would flatten into one unstaged pile.
func (r *Repo) StashPop(ctx context.Context, oid string) ([]string, error) {
	top, err := r.StashRef(ctx)
	if err != nil {
		return nil, err
	}
	if top != oid {
		return nil, ErrStashMoved
	}
	if _, err := r.run(ctx, "stash", "pop", "--index"); err != nil {
		conflicts, cErr := r.ConflictedFiles(ctx)
		if cErr != nil || len(conflicts) == 0 {
			return nil, err
		}
		return conflicts, nil
	}
	return nil, nil
}

// Merge merges ref into the checked-out branch, returning the conflicting paths
// when it stops on a conflict. As with StashPop a conflict is a result, not an
// error: it is the outcome the shelve sequence exists to catch.
//
// --no-edit keeps git from opening an editor for the merge message; run's
// environment closes that door for good (see git.go).
func (r *Repo) Merge(ctx context.Context, ref string) ([]string, error) {
	if _, err := r.run(ctx, "merge", "--no-edit", ref); err != nil {
		conflicts, cErr := r.ConflictedFiles(ctx)
		if cErr != nil || len(conflicts) == 0 {
			return nil, err
		}
		return conflicts, nil
	}
	return nil, nil
}

// MergeAbort restores the pre-merge state after a conflict.
func (r *Repo) MergeAbort(ctx context.Context) error {
	_, err := r.run(ctx, "merge", "--abort")
	return err
}
