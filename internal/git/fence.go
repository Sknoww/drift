package git

// The Drift-managed block, and the file mechanics around it.
//
// Two of Drift's features teach Git a constraint by writing plain text into a
// file Git already reads: a `-merge` declaration in an attributes file
// (attributes.go, area 5) and a held path in $GIT_DIR/info/exclude
// (localonly.go, area 6). Neither file has Git plumbing to write it, and both
// are files the *user* also hand-maintains — so Drift's own lines are fenced.
//
// The fence buys three things: the lines are identifiable, they are removable,
// and a repeat write lands beside its siblings instead of scattered down the
// file. Matching is on the bare prefix, so an edited comment tail never orphans
// the block.
//
// One fence shape across both files on purpose — a user who meets the block in
// info/exclude should recognize the one they already saw in .gitattributes.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	fenceBegin = "# drift:begin"
	fenceEnd   = "# drift:end"
)

// fenceHeaderFor is a block's opening line, naming what the block holds so the
// file explains itself to whoever opens it next.
func fenceHeaderFor(what string) string {
	return fenceBegin + " — " + what + " managed by drift"
}

// fenceRange locates a complete Drift block: the index of its opening line and
// of its closing one, with the block's own entries in between. It reports ok
// only for a begin with a matching end after it, so a stray marker is never
// read as a block whose "contents" are somebody else's rules.
func fenceRange(lines []string) (begin, end int, ok bool) {
	begin = -1
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if begin < 0 && strings.HasPrefix(trimmed, fenceBegin) {
			begin = i
			continue
		}
		if begin >= 0 && strings.HasPrefix(trimmed, fenceEnd) {
			return begin, i, true
		}
	}
	return 0, 0, false
}

// fenceInsertPoint is the line index a new entry goes at — the closing fence of
// the Drift block, so the entry lands as the block's last line.
func fenceInsertPoint(lines []string) (int, bool) {
	_, end, ok := fenceRange(lines)
	return end, ok
}

// splitLines splits file content into lines, dropping the trailing newline so a
// rejoin does not grow a blank line on every write.
func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(content, "\n"), "\n")
}

// joinLines rebuilds file content, always newline-terminated — both files here
// are line-based, and a missing final newline would fuse the next append onto
// the last entry.
func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// writeFileAtomic writes data via temp-file + rename, the same guarantee the
// store gives its JSON: a crash or a full disk can never truncate an existing
// file into a half-written one. An existing file keeps its mode.
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
