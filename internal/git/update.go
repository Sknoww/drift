package git

import (
	"context"
	"fmt"
	"strings"
)

// The update sequence's git calls (roadmap area 17, docs/specs/shelve-sequence.md):
// the three things `u` does that `s` does not — check a branch out, pull the
// branch's own upstream, and publish the result.
//
// Checkout is the reversal ADR 0002 records. "Nothing checks anything out" was a
// standing invariant of this package, and area 17 trades it for the bookkeeping
// the update sequence owns end to end: the caller records where it started and
// returns there on every path, including every halt. The invariant was never
// about checkout being dangerous — it was about a stash belonging to the branch
// it was taken on, and that is now enforced by the return rather than by never
// leaving.

// Checkout switches the working tree to branch.
//
// It is deliberately not forced and takes no `-f`: git refuses to switch when
// the move would overwrite uncommitted work, and that refusal is a result the
// caller needs, not an obstacle to route around. The update sequence stashes
// first, so the ordinary case has nothing to overwrite — but a skip-worktree
// file that differs between the two branches is still enough to make git say no
// (area 6 holds those, and a plain stash cannot see them), and a halt there is
// far better than a hidden local change being written over.
func (r *Repo) Checkout(ctx context.Context, branch string) error {
	_, err := r.run(ctx, "switch", "--quiet", branch)
	return err
}

// upstreamFormat pairs each ref with the remote-tracking ref it follows. Shared
// by the two readers below so the one branch and the whole set are answering
// literally the same question in the same words.
const upstreamFormat = "--format=%(refname:short)%09%(upstream:short)"

// Upstream names the remote-tracking ref a local branch is configured to track,
// returning "" when it tracks nothing. A branch that has never been published
// has no upstream, which is not an error — it is the answer that tells the
// update sequence there is nothing to pull and nowhere to push.
//
// for-each-ref rather than `rev-parse <branch>@{upstream}`, for StashRef's
// reason: a branch with no upstream makes rev-parse exit non-zero, which would
// have to be told apart from a real failure by sniffing an exit code, while
// for-each-ref answers with empty output and exit 0. The line is matched by its
// name rather than taken by position — git's own ref namespace already stops
// `feature` and `feature/thing` from coexisting, so this is reading a keyed
// answer rather than guarding a live hazard.
func (r *Repo) Upstream(ctx context.Context, branch string) (string, error) {
	out, err := r.run(ctx, "for-each-ref", upstreamFormat, "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	for _, l := range lines(out) {
		name, upstream, _ := strings.Cut(l, "\t")
		if name == branch {
			return upstream, nil
		}
	}
	return "", nil
}

// Upstreams is the same question asked of every local branch at once, for the
// dashboard's status sweep: branch name → the ref it tracks, "" when it tracks
// nothing. One shell-out for the whole repo rather than one per row.
//
// **Present-with-an-empty-value is a distinct answer from absent**, and the
// sweep depends on the difference: a branch in the map with no upstream has
// never been published and the row says so, while a branch missing from the map
// does not exist locally and there is nothing to say about it at all. Upstream
// above cannot draw that distinction (both are "") and does not need to — it is
// asked about a branch the sequence has already established is there.
func (r *Repo) Upstreams(ctx context.Context) (map[string]string, error) {
	out, err := r.run(ctx, "for-each-ref", upstreamFormat, "refs/heads")
	if err != nil {
		return nil, err
	}
	upstreams := make(map[string]string)
	for _, l := range lines(out) {
		name, upstream, _ := strings.Cut(l, "\t")
		if name == "" {
			continue
		}
		upstreams[name] = upstream
	}
	return upstreams, nil
}

// PushOutcome is what a push did to the remote branch.
type PushOutcome int

const (
	// PushUpdated: the remote branch moved to the local tip.
	PushUpdated PushOutcome = iota
	// PushUpToDate: the remote branch was already there. Not an error, and worth
	// telling apart from an update — "published" and "already published" are
	// different claims about the same branch.
	PushUpToDate
	// PushRejected: the remote branch moved on after we last read it, so the push
	// is not a fast-forward. Someone else's commit is in the way, which is exactly
	// the class of thing Drift hands back rather than resolving — never a force.
	PushRejected
)

// Push publishes local to remoteBranch on remote.
//
// The refspec is written out in full rather than left to `push.default`: the
// upstream a branch tracks may carry a different name from the branch itself,
// and a push that silently resolved to some other ref would publish the right
// commits to the wrong place.
//
// A rejection is read from `--porcelain`'s flag column, not from git's message
// text — the same rule as everywhere else in this package. Porcelain writes its
// per-ref status to stdout and *then* exits non-zero, which is why this is the
// one caller of runKeepingOutput: without the output a rejected push and an
// unreachable remote are the same non-zero exit, and they need opposite
// responses.
func (r *Repo) Push(ctx context.Context, remote, local, remoteBranch string) (PushOutcome, error) {
	refspec := local + ":refs/heads/" + remoteBranch
	out, err := r.runKeepingOutput(ctx, "push", "--porcelain", remote, refspec)
	outcome, ok := parsePush(out)
	switch {
	case ok && outcome == PushRejected:
		return PushRejected, nil // a rejection is a result; the non-zero exit reports it
	case err != nil:
		return 0, err
	case !ok:
		return 0, fmt.Errorf("git push %s %s: no status line in --porcelain output", remote, refspec)
	}
	return outcome, nil
}

// parsePush reads the status of the one ref pushed out of --porcelain output.
//
// Each status line is `<flag>\t<from>:<to>\t<summary>`, where the flag is a
// single character in column zero: a space for a fast-forward, `+` forced, `*`
// new ref, `=` already up to date, `!` rejected, `-` deleted. The surrounding
// "To <url>" and "Done" lines carry no tab in column one, which is what tells
// them apart without matching on their text.
//
// Deliberately not routed through lines(): it trims, and the flag for the most
// ordinary outcome of all — a plain fast-forward — is a space.
func parsePush(out string) (PushOutcome, bool) {
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSuffix(l, "\r")
		if len(l) < 2 || l[1] != '\t' {
			continue
		}
		switch l[0] {
		case '!':
			return PushRejected, true
		case '=':
			return PushUpToDate, true
		case ' ', '+', '*', '-':
			return PushUpdated, true
		}
	}
	return 0, false
}
