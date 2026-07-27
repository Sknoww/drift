package git

// Writing the `-merge` attribute (roadmap area 5, part 2).
//
// Detection only *reads* Git's declaration; this teaches Git the constraint, so
// Git behaves correctly on a merge even when Drift isn't running. Where it goes
// is the user's choice and both destinations are first-class (CONTEXT.md): the
// repo-root .gitattributes is committed and team-wide, $GIT_DIR/info/attributes
// is local, unversioned, and the highest-precedence attributes source — the path
// for users who cannot commit repo-wide files.
//
// This is the one place the git layer touches a file directly instead of
// shelling out: an attributes file is plain text with no git plumbing to write
// it, and `git check-attr` reads back whatever lands here. The path itself is
// still asked of git (GitDir/TopLevel), never assembled by hand, so a linked
// worktree or submodule stays correct.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// AttrDest is one destination for a written attribute. Both are first-class:
// the choice is the user's, never Drift's.
type AttrDest int

const (
	// AttrRepo is <toplevel>/.gitattributes — committed, shared with the team,
	// and therefore requires commit rights to the repo.
	AttrRepo AttrDest = iota
	// AttrLocal is <git-dir>/info/attributes — local, unversioned, per-repo, and
	// the highest precedence of any attributes source.
	AttrLocal
)

// Label names the destination as the UI shows it.
func (d AttrDest) Label() string {
	if d == AttrLocal {
		return "info/attributes"
	}
	return ".gitattributes"
}

// Detail is the one-line consequence of picking this destination — what the user
// is really choosing between is "the whole team gets this" and "only I do".
func (d AttrDest) Detail() string {
	if d == AttrLocal {
		return "local only, never committed, highest precedence"
	}
	return "committed and shared with the team"
}

// The Drift-managed block. Drift's own lines are fenced so they are identifiable
// and removable, and so a repeat declaration lands beside its siblings instead of
// scattered down a file the user also hand-maintains. Matching is on the bare
// prefix, so an edited comment tail never orphans the block.
const (
	fenceBegin  = "# drift:begin"
	fenceEnd    = "# drift:end"
	fenceHeader = fenceBegin + " — unmergeable declarations managed by drift"
)

// AttrDeclaration reports what a declaration did.
type AttrDeclaration struct {
	Path    string // the attributes file written, or that already declared it
	Pattern string // the pattern as written
	Already bool   // the pattern already declared -merge; nothing was written
}

// TopLevel reports the absolute path of the working tree's root — where a
// committed .gitattributes lives. Asking git keeps it right from any
// subdirectory; a bare repo has no working tree and errors here, which is
// correct, since there is nothing to declare attributes for.
func (r *Repo) TopLevel(ctx context.Context) (string, error) {
	out, err := r.run(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// AttrPath resolves the attributes file for dest, without creating it.
func (r *Repo) AttrPath(ctx context.Context, dest AttrDest) (string, error) {
	switch dest {
	case AttrLocal:
		gitDir, err := r.GitDir(ctx)
		if err != nil {
			return "", err
		}
		return filepath.Join(gitDir, "info", "attributes"), nil
	case AttrRepo:
		top, err := r.TopLevel(ctx)
		if err != nil {
			return "", err
		}
		return filepath.Join(top, ".gitattributes"), nil
	}
	return "", fmt.Errorf("unknown attributes destination %d", dest)
}

// DeclareUnmergeable declares pattern as never-merge — `<pattern> -merge` — in
// the chosen attributes file, creating the file if needed.
//
// `-merge`, never the `binary` macro: binary implies `-diff`, which would kill
// the diff panel. Unmergeable files are still text worth diffing (CONTEXT.md).
//
// A pattern already declared -merge anywhere in that file is left alone and
// reported as Already, so declaring twice is a no-op rather than a duplicate
// line. Nothing else in the file is reordered or rewritten.
func (r *Repo) DeclareUnmergeable(ctx context.Context, dest AttrDest, pattern string) (AttrDeclaration, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return AttrDeclaration{}, fmt.Errorf("declare unmergeable: empty pattern")
	}

	path, err := r.AttrPath(ctx, dest)
	if err != nil {
		return AttrDeclaration{}, err
	}
	decl := AttrDeclaration{Path: path, Pattern: pattern}

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return decl, fmt.Errorf("read %s: %w", path, err)
	}

	updated, already := insertDeclaration(string(existing), pattern)
	decl.Already = already
	if already {
		return decl, nil
	}
	if err := writeFileAtomic(path, []byte(updated)); err != nil {
		return decl, err
	}
	return decl, nil
}

// insertDeclaration returns content with pattern declared -merge, and whether it
// was already declared (in which case content comes back untouched). The new
// line joins the Drift fence when the file has one, and starts a fresh fenced
// block at the end of the file when it does not.
func insertDeclaration(content, pattern string) (string, bool) {
	lines := splitLines(content)
	for _, l := range lines {
		if declaresUnmergeable(l, pattern) {
			return content, true
		}
	}

	line := attrLine(pattern)
	if at, ok := fenceInsertPoint(lines); ok {
		return joinLines(slices.Insert(lines, at, line)), false
	}

	// No fence — or a half-written one, which is hand-mangled and not ours to
	// repair. Either way a complete new block at the end is honest and additive.
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
		lines = append(lines, "")
	}
	lines = append(lines, fenceHeader, line, fenceEnd)
	return joinLines(lines), false
}

// fenceInsertPoint is the line index a new declaration goes at — the closing
// fence of the Drift block, so the line lands as the block's last rule. It
// reports ok only for a complete block (a begin with a matching end after it),
// so a stray marker never turns into an insertion point in the middle of someone
// else's rules.
func fenceInsertPoint(lines []string) (int, bool) {
	begin := -1
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if begin < 0 && strings.HasPrefix(trimmed, fenceBegin) {
			begin = i
			continue
		}
		if begin >= 0 && strings.HasPrefix(trimmed, fenceEnd) {
			return i, true
		}
	}
	return 0, false
}

// attrLine formats one declaration. A pattern containing whitespace is quoted,
// which is how git's own attributes syntax carries one.
func attrLine(pattern string) string {
	if strings.ContainsAny(pattern, " \t") {
		return `"` + pattern + `" -merge`
	}
	return pattern + " -merge"
}

// declaresUnmergeable reports whether one attributes line already sets -merge on
// exactly this pattern. Comments and blank lines never do; a line setting other
// attributes on the same pattern does not either, since it leaves merge alone.
func declaresUnmergeable(line, pattern string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}
	pat, rest := splitAttrLine(trimmed)
	if pat != pattern {
		return false
	}
	return slices.Contains(strings.Fields(rest), "-merge")
}

// splitAttrLine splits an attributes line into its pattern and the attributes
// that follow, unquoting a quoted pattern so `"a b" -merge` compares equal to
// the pattern `a b`.
func splitAttrLine(line string) (pattern, attrs string) {
	if strings.HasPrefix(line, `"`) {
		if end := strings.Index(line[1:], `"`); end >= 0 {
			return line[1 : end+1], strings.TrimSpace(line[end+2:])
		}
	}
	pattern, attrs, _ = strings.Cut(line, " ")
	return strings.TrimSpace(pattern), strings.TrimSpace(attrs)
}

// splitLines splits file content into lines, dropping the trailing newline so a
// rejoin does not grow a blank line on every write.
func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(content, "\n"), "\n")
}

// joinLines rebuilds file content, always newline-terminated — an attributes
// file is line-based, and a missing final newline would fuse the next append
// onto the last rule.
func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// writeFileAtomic writes data via temp-file + rename, the same guarantee the
// store gives its JSON: a crash or a full disk can never truncate an existing
// attributes file into a half-written one. An existing file keeps its mode.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	defer os.Remove(tmp.Name()) // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
