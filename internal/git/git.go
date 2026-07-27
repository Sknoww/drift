// Package git is a thin wrapper over the git binary. Every call here shells out
// and parses machine-readable output; nothing checks anything out, and nothing
// touches the files a branch is made of.
//
// The one deliberate exception is attributes.go, which writes a `-merge`
// declaration into an attributes file (roadmap area 5). That is Drift teaching
// git a constraint, not Drift editing the user's work, and git offers no
// plumbing to write it — but the file's location is still asked of git, never
// assembled by hand.
package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
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

// noEditor keeps a git subprocess from ever launching an editor. Drift owns the
// terminal: a merge that opens vim into the middle of a Bubble Tea render
// corrupts the display and strands the user in an editor they did not ask for and
// may not know how to leave. `--no-edit` covers the merge message specifically;
// this closes the door for every call, including the ones that would open an
// editor for a reason Drift did not anticipate. Set in one place rather than per
// call site, so a new shell-out inherits it rather than having to remember it.
var noEditor = []string{"GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true"}

func (r *Repo) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.Dir
	cmd.Env = append(os.Environ(), noEditor...)
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

// RemoteBranches lists every remote-tracking branch by its short name
// (e.g. "origin/main"), in git's own sort order. These are the refs the
// first-run wizard offers as targets: a target is compared against its
// origin/<name> ref, so offering local branches would produce targets that
// silently compare against the wrong thing (roadmap area 4).
//
// The "<remote>/HEAD" symref — which %(refname:short) shortens to the bare
// remote name, e.g. "origin" — is dropped: it is a pointer to a remote's
// default branch, not a branch to pick. Every real remote-tracking branch
// shortens to "<remote>/<branch>", so requiring a slash filters it out.
func (r *Repo) RemoteBranches(ctx context.Context) ([]string, error) {
	out, err := r.run(ctx, "for-each-ref", "--format=%(refname:short)", "refs/remotes")
	if err != nil {
		return nil, err
	}
	var got []string
	for _, b := range lines(out) {
		if !strings.Contains(b, "/") {
			continue
		}
		got = append(got, b)
	}
	return got, nil
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

// ChangedFiles lists the repo-relative paths that differ between base and tip
// using the three-dot form `git diff --name-only base...tip`, i.e. what tip
// changed since the two diverged (their merge base). Feeding it (branch,
// targetRef) yields what the target moved; (targetRef, branch) yields what the
// branch changed. Both halves feed area 5's collision set and, later, area 7's
// pre-merge check. Paths are git's own forward-slash form, ready for glob
// matching regardless of OS.
func (r *Repo) ChangedFiles(ctx context.Context, base, tip string) ([]string, error) {
	out, err := r.run(ctx, "diff", "--name-only", base+"..."+tip)
	if err != nil {
		return nil, err
	}
	return lines(out), nil
}

// WorkingTreeModified lists tracked paths that differ from HEAD — staged or
// not. It is the working-tree half of "local edits" for the checked-out branch:
// a `git stash` (no -u) captures exactly these, so they are the uncommitted
// changes that can turn into a conflict when the shelve sequence pops them back
// over a freshly merged target. Untracked files are excluded deliberately —
// plain stash leaves them in place, so they ride the merge through untouched.
func (r *Repo) WorkingTreeModified(ctx context.Context) ([]string, error) {
	out, err := r.run(ctx, "diff", "--name-only", "HEAD")
	if err != nil {
		return nil, err
	}
	return lines(out), nil
}

// FileDiff returns the plain-text diff of one path between base and tip, in the
// same three-dot sense as ChangedFiles: `git diff base...tip -- path`. Fed
// (branch, targetRef, path) it is exactly the incoming upstream change the user
// must reconcile by hand — the diff the area-5 panel shows in place of hunting
// for it in a web UI. The pathspec is passed after `--` so a path that looks
// like a flag is never misread.
func (r *Repo) FileDiff(ctx context.Context, base, tip, path string) (string, error) {
	return r.run(ctx, "diff", base+"..."+tip, "--", path)
}

// CheckAttrMerge reports which of the given paths Git is told never to merge —
// the `-merge` attribute, which `git check-attr` renders as merge "unset". This
// is the .gitattributes half of the hybrid unmergeable rule (CONTEXT.md); the
// config globs are the additive other half, resolved separately. NUL-delimited
// output (-z) is parsed so a path containing a colon or space is never
// mis-split. An empty path list is a no-op — check-attr with no paths would
// otherwise block reading its stdin.
func (r *Repo) CheckAttrMerge(ctx context.Context, paths []string) (map[string]bool, error) {
	unmergeable := make(map[string]bool)
	if len(paths) == 0 {
		return unmergeable, nil
	}
	args := append([]string{"check-attr", "-z", "merge", "--"}, paths...)
	out, err := r.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	// -z output is a flat stream of NUL-terminated fields, three per record:
	// <path> NUL <attribute> NUL <value> NUL. A trailing empty field follows the
	// final NUL, so drop it before grouping into triples.
	fields := strings.Split(out, "\x00")
	if n := len(fields); n > 0 && fields[n-1] == "" {
		fields = fields[:n-1]
	}
	for i := 0; i+2 < len(fields); i += 3 {
		if fields[i+2] == "unset" {
			unmergeable[fields[i]] = true
		}
	}
	return unmergeable, nil
}

// GitDir reports the absolute path of the repository's git directory. Asking
// git rather than joining <root>/.git is what makes this correct in a linked
// worktree or a submodule, where .git is a file pointing elsewhere.
func (r *Repo) GitDir(ctx context.Context) (string, error) {
	out, err := r.run(ctx, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
