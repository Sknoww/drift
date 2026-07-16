// Package git is a thin wrapper over the git binary. Every call shells out and
// parses machine-readable output; nothing here checks anything out or mutates
// the working tree.
package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Repo runs git commands against one working directory.
type Repo struct {
	Dir string
}

// New returns a Repo rooted at dir. Any directory inside the repo works; git
// walks up to find the repository itself.
func New(dir string) *Repo {
	return &Repo{Dir: dir}
}

func (r *Repo) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.Dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, msg)
	}
	return stdout.String(), nil
}

// lines splits command output into non-empty trimmed lines.
func lines(out string) []string {
	var got []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			got = append(got, l)
		}
	}
	return got
}

// LocalBranches lists every local branch, in git's own sort order.
func (r *Repo) LocalBranches(ctx context.Context) ([]string, error) {
	out, err := r.run(ctx, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return nil, err
	}
	return lines(out), nil
}

// CurrentBranch reports the checked-out branch, or "" when HEAD is detached.
func (r *Repo) CurrentBranch(ctx context.Context) (string, error) {
	out, err := r.run(ctx, "branch", "--show-current")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// IsDirty reports whether the working tree has uncommitted changes. This is a
// property of the checked-out branch alone — no other branch can be dirty.
func (r *Repo) IsDirty(ctx context.Context) (bool, error) {
	out, err := r.run(ctx, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// AheadBehind is a branch's divergence from a target ref.
type AheadBehind struct {
	Ahead  int // commits on the branch not yet on the target
	Behind int // commits on the target not in the branch — the target moved underneath
}

// AheadBehind compares branch against targetRef without checking anything out.
// Run Fetch first if the target is a remote ref and the answer must be current.
func (r *Repo) AheadBehind(ctx context.Context, branch, targetRef string) (AheadBehind, error) {
	out, err := r.run(ctx, "rev-list", "--left-right", "--count", targetRef+"..."+branch)
	if err != nil {
		return AheadBehind{}, err
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return AheadBehind{}, fmt.Errorf("rev-list %s...%s: unexpected output %q", targetRef, branch, out)
	}
	behind, err := strconv.Atoi(fields[0])
	if err != nil {
		return AheadBehind{}, fmt.Errorf("rev-list %s...%s: behind count %q: %w", targetRef, branch, fields[0], err)
	}
	ahead, err := strconv.Atoi(fields[1])
	if err != nil {
		return AheadBehind{}, fmt.Errorf("rev-list %s...%s: ahead count %q: %w", targetRef, branch, fields[1], err)
	}
	return AheadBehind{Ahead: ahead, Behind: behind}, nil
}

// CandidateBranches returns local branches whose name contains ticketID,
// case-insensitively. It pre-filters the pairing flow; the user still confirms
// every match. An empty ticketID matches nothing rather than everything.
func (r *Repo) CandidateBranches(ctx context.Context, ticketID string) ([]string, error) {
	if strings.TrimSpace(ticketID) == "" {
		return nil, nil
	}
	all, err := r.LocalBranches(ctx)
	if err != nil {
		return nil, err
	}
	id := strings.ToLower(strings.TrimSpace(ticketID))
	var got []string
	for _, b := range all {
		if strings.Contains(strings.ToLower(b), id) {
			got = append(got, b)
		}
	}
	return got, nil
}

// Fetch updates remote-tracking refs so AheadBehind reflects the server.
func (r *Repo) Fetch(ctx context.Context) error {
	_, err := r.run(ctx, "fetch", "--quiet")
	return err
}
