package git

// Holding a change on this machine only (roadmap area 6).
//
// Two primitives, routed by whether Git tracks the path — the user marks a
// *change*, never a mechanism (docs/specs/local-only-changes.md):
//
//	tracked   → git update-index --skip-worktree, so Git treats the file as
//	            unmodified: absent from status, ignored by `add -A` and `commit -am`.
//	untracked → a Drift-fenced block in $GIT_DIR/info/exclude, so the path is
//	            ignored locally and can never be swept into a commit by accident.
//
// Git's own flags are the source of truth, exactly as with unmergeables:
// nothing here writes a registry of what is held. SkipWorktreeFiles and
// ExcludedPaths read the answer back out of Git every time, so Drift cannot
// fall out of sync and Git stays correct when Drift isn't running.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// excludeHeader opens the Drift block in info/exclude. Its fence is the same one
// the attributes file uses (fence.go) — one Drift block shape, wherever a user
// meets it.
var excludeHeader = fenceHeaderFor("local-only paths")

// WorkingChange is one path Git currently reports as changed: a tracked file
// that differs from HEAD, or an untracked file sitting in the tree. It is what
// the add flow offers — you hold a change you can *see*, never a path typed
// from memory.
type WorkingChange struct {
	Path    string
	Tracked bool // false routes the hold to info/exclude rather than skip-worktree
	Staged  bool // the change is in the index, where skip-worktree cannot hold it
}

// runAtTop runs a git command with the working tree's root as its working
// directory.
//
// Two commands here need it. `update-index` resolves the filenames it is given
// against the *current* directory rather than the repo root, and `ls-files`
// limits itself to the directory it runs in — so a Drift invoked from a
// subdirectory would fail to hold a repo-relative path, and would report only
// part of the held set. Running them from the top makes every path in this file
// repo-relative, in and out. (`status --porcelain` needs no such help: its
// paths are relative to the root by definition.)
func (r *Repo) runAtTop(ctx context.Context, args ...string) (string, error) {
	top, err := r.TopLevel(ctx)
	if err != nil {
		return "", err
	}
	return r.run(ctx, append([]string{"-C", top}, args...)...)
}

// SetSkipWorktree tells Git to treat a tracked path as unmodified no matter
// what the working tree says — the hold for a tracked file. It fails for a path
// Git does not track, which is correct: that path belongs in info/exclude.
func (r *Repo) SetSkipWorktree(ctx context.Context, path string) error {
	_, err := r.runAtTop(ctx, "update-index", "--skip-worktree", "--", path)
	return err
}

// ClearSkipWorktree releases a tracked hold. The local edits were never lost —
// they reappear at once as ordinary working-tree changes, leaving the user to
// commit or discard them.
func (r *Repo) ClearSkipWorktree(ctx context.Context, path string) error {
	_, err := r.runAtTop(ctx, "update-index", "--no-skip-worktree", "--", path)
	return err
}

// SkipWorktreeFiles lists the tracked paths currently held by the skip-worktree
// bit — Git's own answer to "what am I hiding from you", read back rather than
// remembered.
//
// `ls-files -v` tags every index entry with a letter; S is skip-worktree. NUL
// termination (-z) keeps a path containing a space or a quote intact. A path
// that also carries assume-unchanged is tagged lowercase and so is not listed,
// which is right: Drift never sets that flag (the spec rules it out as a
// performance hint Git may clear on its own), so such an entry was not held by
// Drift.
func (r *Repo) SkipWorktreeFiles(ctx context.Context) ([]string, error) {
	out, err := r.runAtTop(ctx, "ls-files", "-v", "-z")
	if err != nil {
		return nil, err
	}
	var held []string
	for _, rec := range strings.Split(out, "\x00") {
		// Each record is "<tag><space><path>".
		if len(rec) < 3 || rec[0] != 'S' {
			continue
		}
		held = append(held, rec[2:])
	}
	return held, nil
}

// WorkingChanges lists what Git currently sees changed in the working tree:
// tracked files that differ from HEAD, and untracked files. Held paths are
// absent by construction — a skip-worktree file reads as unmodified and an
// excluded file is ignored — so this is already "what could be held next".
//
// The -z form is parsed rather than plain --porcelain because that is the only
// one that leaves paths unquoted and unambiguous. --untracked-files=all is
// equally load-bearing: git's default collapses a wholly-untracked directory to
// a single "service/" entry, and a hold is on a *file* — offering the directory
// would hold every path under it, including ones the user never looked at.
func (r *Repo) WorkingChanges(ctx context.Context) ([]WorkingChange, error) {
	out, err := r.run(ctx, "status", "--porcelain", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}

	var changes []WorkingChange
	records := strings.Split(out, "\x00")
	for i := 0; i < len(records); i++ {
		rec := records[i]
		// Each record is "XY <path>": the index status, the worktree status, a
		// space, then the path.
		if len(rec) < 4 {
			continue
		}
		x, path := rec[0], rec[3:]
		if x == 'R' || x == 'C' {
			i++ // a rename or copy spends a second field on the original path
		}
		untracked := rec[:2] == "??"
		changes = append(changes, WorkingChange{
			Path:    path,
			Tracked: !untracked,
			// A staged change lives in the index, and skip-worktree does not stop
			// the index from being committed — so this is the one candidate the
			// hold cannot honestly protect. Reported, never silently held.
			Staged: !untracked && x != ' ',
		})
	}
	return changes, nil
}

// ExcludePath resolves $GIT_DIR/info/exclude — the per-repo, unversioned
// .gitignore Git already provides — without creating it. Asking git for the git
// dir is what keeps this right in a linked worktree or a submodule.
func (r *Repo) ExcludePath(ctx context.Context) (string, error) {
	gitDir, err := r.GitDir(ctx)
	if err != nil {
		return "", err
	}
	return filepath.Join(gitDir, "info", "exclude"), nil
}

// ExcludedPaths lists the untracked paths held inside Drift's fenced block.
// Only the block: rules the user wrote by hand are theirs, and are neither
// reported as held nor ever touched.
func (r *Repo) ExcludedPaths(ctx context.Context) ([]string, error) {
	path, err := r.ExcludePath(ctx)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil // no exclude file yet is an empty held set, not an error
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return excludedIn(string(data)), nil
}

// AddExclude holds an untracked path by adding it to Drift's block, creating
// the file and the block if needed. A path already in the block is a no-op
// rather than a duplicate line.
func (r *Repo) AddExclude(ctx context.Context, path string) error {
	return r.rewriteExclude(ctx, path, insertExclude)
}

// RemoveExclude releases an untracked hold. The file itself is untouched — it
// simply shows up as untracked again.
func (r *Repo) RemoveExclude(ctx context.Context, path string) error {
	return r.rewriteExclude(ctx, path, removeExclude)
}

// rewriteExclude applies one edit to the exclude file, atomically, and writes
// nothing at all when the edit changes nothing.
func (r *Repo) rewriteExclude(ctx context.Context, path string, edit func(content, path string) (string, bool)) error {
	file, err := r.ExcludePath(ctx)
	if err != nil {
		return err
	}
	existing, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", file, err)
	}
	updated, changed := edit(string(existing), path)
	if !changed {
		return nil
	}
	return writeFileAtomic(file, []byte(updated))
}

// excludedIn reads the paths out of Drift's block, undoing the pattern encoding
// so what comes back is the repo-relative path that went in.
func excludedIn(content string) []string {
	lines := splitLines(content)
	begin, end, ok := fenceRange(lines)
	if !ok {
		return nil
	}
	var out []string
	for _, l := range lines[begin+1 : end] {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		out = append(out, excludedPath(trimmed))
	}
	return out
}

// insertExclude returns content with path held, and whether anything changed.
// The entry joins the Drift block when the file has one, and starts a fresh
// block at the end of the file when it does not — the same shape, and the same
// reasoning, as insertDeclaration.
func insertExclude(content, path string) (string, bool) {
	pattern := excludePattern(path)
	lines := splitLines(content)

	if begin, end, ok := fenceRange(lines); ok {
		for _, l := range lines[begin+1 : end] {
			if strings.TrimSpace(l) == pattern {
				return content, false // already held; nothing to write
			}
		}
		at, _ := fenceInsertPoint(lines)
		return joinLines(slices.Insert(lines, at, pattern)), true
	}

	// No block — or a half-written one, which is hand-mangled and not ours to
	// repair. Either way a complete new block at the end is honest and additive.
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
		lines = append(lines, "")
	}
	lines = append(lines, excludeHeader, pattern, fenceEnd)
	return joinLines(lines), true
}

// removeExclude returns content with path released, and whether anything
// changed. A block left empty is removed outright rather than kept as two
// markers around nothing, so releasing the last held path puts the file back
// the way the user had it.
func removeExclude(content, path string) (string, bool) {
	pattern := excludePattern(path)
	lines := splitLines(content)
	begin, end, ok := fenceRange(lines)
	if !ok {
		return content, false
	}

	var kept []string
	found := false
	for _, l := range lines[begin+1 : end] {
		if !found && strings.TrimSpace(l) == pattern {
			found = true
			continue
		}
		kept = append(kept, l)
	}
	if !found {
		return content, false
	}

	out := append([]string{}, lines[:begin]...)
	if len(kept) > 0 {
		out = append(out, lines[begin]) // the header, exactly as it was
		out = append(out, kept...)
		out = append(out, lines[end])
	} else {
		// The block is gone; so is the blank line that separated it from the
		// user's own rules.
		for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
			out = out[:len(out)-1]
		}
	}
	out = append(out, lines[end+1:]...)
	return joinLines(out), true
}

// excludePattern renders a repo-relative path as a gitignore pattern matching
// exactly that one file.
//
// The leading slash is load-bearing. A gitignore pattern with no slash in it
// matches a *basename at any depth*, so a plain "config.yml" would quietly hold
// back every config.yml in the tree. Anchoring the pattern to the exclude
// file's base — the repo root, for info/exclude — makes it the one path the
// user actually picked. It also means a name beginning with "#" or "!" can
// never be read as a comment or a negation.
//
// Glob metacharacters in the name are escaped, so a file literally called
// "a[1].txt" is held rather than a pattern that merely happens to match it.
func excludePattern(path string) string {
	var b strings.Builder
	b.WriteByte('/')
	for _, r := range path {
		switch r {
		case '\\', '*', '?', '[':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	pattern := b.String()
	// Git strips trailing whitespace from a pattern unless it is escaped, so a
	// name that really ends in a space would otherwise hold the wrong path.
	if n := len(pattern); pattern[n-1] == ' ' || pattern[n-1] == '\t' {
		pattern = pattern[:n-1] + `\` + pattern[n-1:]
	}
	return pattern
}

// excludedPath is excludePattern's inverse: the repo-relative path a Drift
// entry stands for. Reading it back rather than storing it separately is what
// keeps the exclude file the single source of truth for untracked holds.
func excludedPath(pattern string) string {
	pattern = strings.TrimPrefix(pattern, "/")
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '\\' && i+1 < len(pattern) {
			i++ // the backslash escapes whatever follows; keep only that
		}
		b.WriteByte(pattern[i])
	}
	return b.String()
}
