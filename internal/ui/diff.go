package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Sknoww/drift/internal/git"
	"github.com/Sknoww/drift/internal/store"
)

// The unmergeable diff panel (roadmap area 5). It answers the one question the
// old workflow made you open a web UI for: "the target moved under me and
// touched a file I can't merge — what exactly changed?" The colliding paths were
// found during the sweep; this screen fetches and shows each one's incoming diff,
// per branch, because MVP2 and MVP3 can hold different versions of the same file.

// openDiff enters the diff panel for the branch at row. A branch with no
// unmergeable collision has nothing to reconcile, so it stays on the dashboard
// with a note rather than opening an empty panel.
func (m Model) openDiff(row rowRef) (tea.Model, tea.Cmd) {
	t := m.store.Tickets[row.ticket]
	br := t.Branches[row.branch]
	st := m.status[statusKey(t.ID, br.Branch)]

	if len(st.unmergeable) == 0 {
		m.notice = "no unmergeable changes on " + br.Branch
		return m, nil
	}
	target, ok := m.cfg.Target(br.TargetKey)
	if !ok {
		// A stale pairing: the row already shows the warning, so just say why.
		m.notice = "unknown target " + quote(br.TargetKey) + " for " + br.Branch
		return m, nil
	}

	m.diff = diffState{
		ticketID:  t.ID,
		branch:    br.Branch,
		targetKey: br.TargetKey,
		targetRef: target.Ref,
		// Copied, not aliased: declaring updates each file's badge in place, and
		// that must not reach back into the sweep's own result. The dashboard
		// picks the change up from git on its next sweep.
		files:  append([]collision(nil), st.unmergeable...),
		cursor: 0,
		cache:  make(map[string]diffEntry),
		vp:     viewport.New(diffViewportWidth(m.styles, m.width), diffViewportHeight(m.height)),
	}
	m.screen = screenDiff
	m.notice = ""
	return m.loadCurrentDiff()
}

// dispatchDiff runs one named action on the diff panel. Scrolling is not here —
// unbound keys reach the viewport directly (handleKey), so j/k and the page keys
// scroll without a binding.
func (m Model) dispatchDiff(action Action) (tea.Model, tea.Cmd) {
	if m.diff.declare.open {
		return m.dispatchDeclare(action)
	}
	switch action {
	case ActionCancel:
		m.screen = screenDashboard
		m.diff = diffState{}
		return m, nil
	case ActionNextFile:
		return m.moveDiffFile(1)
	case ActionPrevFile:
		return m.moveDiffFile(-1)
	case ActionDeclare:
		return m.openDeclare()
	}
	return m, nil
}

// moveDiffFile steps to another colliding file and shows its diff (fetching it
// the first time). The list wraps in both directions: reconciling a branch's
// collisions is a round trip, not a walk to a dead end, so tab past the last
// file returns to the first and shift+tab from the first reaches the last.
func (m Model) moveDiffFile(delta int) (tea.Model, tea.Cmd) {
	n := len(m.diff.files)
	if n == 0 {
		return m, nil
	}
	m.diff.cursor = ((m.diff.cursor+delta)%n + n) % n
	return m.loadCurrentDiff()
}

// loadCurrentDiff shows the selected file's diff. A cached diff renders at once;
// otherwise the panel shows a loading line and a Cmd fetches it off-thread. The
// viewport is reset to the top so each file starts from its first line.
func (m Model) loadCurrentDiff() (Model, tea.Cmd) {
	path := m.diff.files[m.diff.cursor].path
	if entry, ok := m.diff.cache[path]; ok {
		m.diff.vp.SetContent(m.diffBody(entry))
		m.diff.vp.GotoTop()
		return m, nil
	}
	m.diff.vp.SetContent(m.styles.hint.Render("loading diff…"))
	m.diff.vp.GotoTop()
	return m, loadDiffCmd(m.repo, m.diff.branch, m.diff.targetRef, path)
}

// applyDiff folds a fetched diff into the cache. A diff for a branch or target
// the user has since left is discarded, so a slow fetch can never paint the
// wrong branch's changes into the panel.
func (m Model) applyDiff(msg diffMsg) Model {
	if m.screen != screenDiff || msg.branch != m.diff.branch || msg.targetRef != m.diff.targetRef {
		return m
	}
	entry := diffEntry{content: msg.content, err: msg.err}
	m.diff.cache[msg.path] = entry
	if m.diff.files[m.diff.cursor].path == msg.path {
		m.diff.vp.SetContent(m.diffBody(entry))
		m.diff.vp.GotoTop()
	}
	return m
}

// diffBody is what the viewport shows for one file: the raw diff, or the reason
// it is absent. An empty diff is possible (the file's change is only in git's
// eyes, e.g. mode bits), so it is called out rather than left blank.
func (m Model) diffBody(entry diffEntry) string {
	if entry.err != nil {
		return m.styles.errText.Render("failed to load diff: " + entry.err.Error())
	}
	if strings.TrimSpace(entry.content) == "" {
		return m.styles.hint.Render("(no textual diff)")
	}
	return colorizeDiff(m.styles, entry.content)
}

// colorizeDiff colors a unified diff by line role. This is diff-level structure,
// not format-level rendering: what stays permanently out of scope is
// *understanding* an unmergeable format (drawing a Unity scene tree, a workflow
// graph — DESIGN.md §2). Telling an added line from a removed one is what any
// diff reader needs, whatever the file happens to be, and reading the incoming
// change is the entire job of this panel.
func colorizeDiff(s styles, raw string) string {
	lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")
	for i, line := range lines {
		lines[i] = colorizeDiffLine(s, line)
	}
	return strings.Join(lines, "\n")
}

func colorizeDiffLine(s styles, line string) string {
	switch diffLineRole(line) {
	case diffAdded:
		return s.diffAdd.Render(line)
	case diffRemoved:
		return s.diffDel.Render(line)
	case diffHunk:
		return s.diffHunk.Render(line)
	case diffMeta:
		return s.diffMeta.Render(line)
	}
	return line
}

// diffRole is what one line of a unified diff is. Kept separate from the styles
// so the classification is testable on its own — in a test the color profile is
// ASCII and every style renders identically, which would hide exactly the kind
// of mistake the +++/--- rule below exists to prevent.
type diffRole int

const (
	diffContext diffRole = iota // a leading space: the quiet background
	diffAdded                   // +
	diffRemoved                 // -
	diffHunk                    // @@ … @@ — where you are in the file
	diffMeta                    // git's bookkeeping around the hunks
)

// diffMetaPrefixes are the header lines git prints around the hunks: the file
// pair, blob hashes, mode and rename bookkeeping, the no-trailing-newline note.
// None is part of the change itself, so all of them recede.
var diffMetaPrefixes = []string{
	"+++", "---", // tested first — see diffLineRole
	"diff --git", "index ", "new file", "deleted file",
	"old mode", "new mode", "similarity index", "rename ", "Binary files", `\ No newline`,
}

// diffLineRole classifies one line. The file headers `+++` and `---` are matched
// before the bare `+`/`-` they begin with, or every diff would open with one
// line falsely colored as added and one as removed.
func diffLineRole(line string) diffRole {
	for _, prefix := range diffMetaPrefixes {
		if strings.HasPrefix(line, prefix) {
			return diffMeta
		}
	}
	switch {
	case strings.HasPrefix(line, "@@"):
		return diffHunk
	case strings.HasPrefix(line, "+"):
		return diffAdded
	case strings.HasPrefix(line, "-"):
		return diffRemoved
	}
	return diffContext
}

// Declaring a file unmergeable to git itself (area 5, part 2). Detection reads
// the `-merge` attribute; this writes it, so git behaves correctly on a merge
// even when Drift isn't running. It starts here because the diff panel is where
// the user is already looking at the file — and because a file that reached this
// panel is one Drift already knows must never be merged.

// declareDestByName maps a config destination name to the mechanism behind it.
// The names belong to store (it owns what config means), the mechanism to git,
// and only this layer needs both.
var declareDestByName = map[string]git.AttrDest{
	store.DestShared: git.AttrRepo,
	store.DestLocal:  git.AttrLocal,
}

// allDeclareDests is what a repo that says nothing gets: both, in CONTEXT.md's
// order. Shared-with-the-team and local-only are different answers to a real
// question (do I have commit rights? should the team inherit this?), so neither
// is a default and both are shown with their consequence.
var allDeclareDests = []git.AttrDest{git.AttrRepo, git.AttrLocal}

// declareDests is the destinations this repo allows. A config that allow-lists
// them filters *and* orders this list, so a team without a committed
// .gitattributes never sees it offered and cannot pick it by accident.
func declareDests(cfg store.Config) []git.AttrDest {
	allowed := cfg.DeclareDestinations()
	if allowed == nil {
		return allDeclareDests
	}
	var out []git.AttrDest
	for _, name := range allowed {
		if dest, ok := declareDestByName[name]; ok {
			out = append(out, dest)
		}
	}
	return out
}

// openDeclare opens the overlay for the file currently on screen.
func (m Model) openDeclare() (tea.Model, tea.Cmd) {
	if len(m.diff.files) == 0 {
		return m, nil
	}
	dests := declareDests(m.cfg)
	if len(dests) == 0 {
		// Only reachable from a config that validate() would have rejected, but
		// say so rather than open an overlay with nowhere to write.
		m.notice = "no declare destination allowed by this repo's config"
		return m, nil
	}

	path := m.diff.files[m.diff.cursor].path
	m.diff.declare = declareState{
		open:     true,
		step:     stepPattern,
		path:     path,
		patterns: declarePatterns(m.cfg, path),
		dests:    dests,
	}
	m.notice = ""
	return m, nil
}

// declarePatterns is what Drift offers to write for one file: every config glob
// that matched it — declaring the whole class in one line — and the file's own
// path, which declares just this file. A file flagged only by `check-attr`
// (already declared somewhere git can see) still offers its path, so the user
// can pin it into a destination of their choosing.
func declarePatterns(cfg store.Config, path string) []declarePattern {
	var out []declarePattern
	seen := make(map[string]bool)
	for _, match := range cfg.UnmergeableMatches(path) {
		if seen[match.Glob] {
			continue
		}
		seen[match.Glob] = true
		why := "config: " + match.Name
		if strings.TrimSpace(match.Name) == "" {
			why = "config glob"
		}
		out = append(out, declarePattern{pattern: match.Glob, why: why})
	}
	if !seen[path] {
		out = append(out, declarePattern{pattern: path, why: "this file only"})
	}
	return out
}

// dispatchDeclare runs one action inside the overlay. Cancel means "back out one
// step" — from the destination back to the pattern, and from the pattern back to
// the diff — so esc unwinds the choices in the order they were made.
func (m Model) dispatchDeclare(action Action) (tea.Model, tea.Cmd) {
	switch action {
	case ActionCancel:
		if m.diff.declare.step == stepDest {
			m.diff.declare.step = stepPattern
			m.diff.declare.cursor = m.declarePatternIndex()
			return m, nil
		}
		m.diff.declare = declareState{}
		return m, nil

	case ActionMoveUp:
		if m.diff.declare.cursor > 0 {
			m.diff.declare.cursor--
		}
		return m, nil

	case ActionMoveDown:
		if m.diff.declare.cursor < m.diff.declare.count()-1 {
			m.diff.declare.cursor++
		}
		return m, nil

	case ActionConfirm:
		return m.confirmDeclare()
	}
	return m, nil
}

// confirmDeclare commits the current step: the first picks the pattern and moves
// on to the destination, the second closes the overlay and writes.
func (m Model) confirmDeclare() (tea.Model, tea.Cmd) {
	d := m.diff.declare
	if d.count() == 0 {
		return m, nil
	}
	if d.step == stepPattern {
		m.diff.declare.pattern = d.patterns[d.cursor].pattern
		m.diff.declare.step = stepDest
		m.diff.declare.cursor = 0
		return m, nil
	}

	dest := d.dests[d.cursor]
	pattern := d.pattern
	m.diff.declare = declareState{}
	m.notice = "declaring " + pattern + "…"
	return m, declareCmd(m.repo, dest, pattern)
}

// declarePatternIndex recovers which pattern row the chosen pattern is, so
// stepping back from the destination lands on the choice just made rather than
// at the top of the list.
func (m Model) declarePatternIndex() int {
	for i, p := range m.diff.declare.patterns {
		if p.pattern == m.diff.declare.pattern {
			return i
		}
	}
	return 0
}

// count is how many rows the current step lists.
func (d declareState) count() int {
	if d.step == stepDest {
		return len(d.dests)
	}
	return len(d.patterns)
}

// applyDeclare reports what a write did. A pattern already declared is a
// success, not an error — the whole point is that git ends up knowing, and it
// already did.
//
// Then it asks git what the panel's files look like now. That re-read is what
// makes the action visible: the file's badge flips from "not declared" to
// "declared", and a glob covering several listed files flips all of them. Since
// the answer comes from git rather than from what Drift assumes it achieved,
// the badge cannot drift from the truth.
func (m Model) applyDeclare(msg declareMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.notice = "declare failed: " + msg.err.Error()
		return m, nil
	}

	notice := "declared " + msg.decl.Pattern + " -merge in " + msg.dest.Label()
	if msg.decl.Already {
		notice = msg.decl.Pattern + " -merge was already in " + msg.dest.Label()
	}

	var cmds []tea.Cmd
	if m.screen == screenDiff && len(m.diff.files) > 0 {
		cmds = append(cmds, recheckDeclaredCmd(m.repo, m.diff.branch, m.diff.targetRef, paths(m.diff.files)))
	}
	// A shared write lands in the working tree, so the dashboard's dirty dot is
	// stale the moment it happens. A local write touches nothing git tracks.
	if msg.dest == git.AttrRepo && !msg.decl.Already {
		model, cmd := m.startSweep(false)
		m = model.(Model)
		cmds = append(cmds, cmd)
	}

	m.notice = notice // last: startSweep clears it, and the result is the news
	return m, tea.Batch(cmds...)
}

// applyDeclared folds git's re-read of the `-merge` attribute into the panel. A
// reply for a branch the user has since left is discarded, the same guard the
// diff itself uses.
func (m Model) applyDeclared(msg declaredMsg) Model {
	if msg.err != nil || m.screen != screenDiff {
		return m
	}
	if msg.branch != m.diff.branch || msg.targetRef != m.diff.targetRef {
		return m
	}
	// A new slice, not an edit in place: Model is passed by value, so writing
	// through the old backing array would reach every copy of it — including the
	// one a discarded message was supposed to leave untouched.
	files := make([]collision, len(m.diff.files))
	copy(files, m.diff.files)
	for i := range files {
		files[i].declared = msg.byPath[files[i].path]
	}
	m.diff.files = files
	return m
}

// diffView renders the panel: which file of how many, the branch→target it
// reconciles, and the scrollable diff itself.
func (m Model) diffView() string {
	if m.diff.declare.open {
		return m.declareView()
	}
	d := m.diff
	pos := fmt.Sprintf("%d/%d", d.cursor+1, len(d.files))
	head := m.styles.hint.Render("unmergeable "+pos) + "  " +
		m.styles.target.Render(d.branch+" → "+d.targetKey)
	file := m.styles.unmerge.Render(d.files[d.cursor].path) + "  " + m.declaredBadge(d.files[d.cursor])

	body := head + "\n" + file + "\n\n" + d.vp.View()
	help := m.styles.help.Render("tab/⇧tab file · j/k scroll · w declare · esc back · ? help · q quit")
	return m.screenView(body, help)
}

// declaredBadge says whether Git itself has been told never to merge this file,
// or whether only Drift's config globs know. It is the whole answer to "what
// does declaring do, and did anything happen?": the badge names the state, `w`
// changes it, and the badge flips. Without it, declaring a file the panel
// already flags as unmergeable leaves the screen looking identical.
func (m Model) declaredBadge(c collision) string {
	if c.declared {
		return m.styles.help.Render("✓ declared to git")
	}
	return m.styles.unmerge.Render("not declared to git — w declares it")
}

// declareView draws the overlay in the panel's place, the same way the target
// picker replaces the pairing checklist while it is open.
func (m Model) declareView() string {
	d := m.diff.declare
	if d.step == stepDest {
		help := m.styles.help.Render("j/k move · enter write · esc back")
		return m.screenView(m.declareDestBody(), help)
	}
	help := m.styles.help.Render("j/k move · enter choose · esc cancel")
	return m.screenView(m.declarePatternBody(), help)
}

// declarePatternBody asks what to declare, showing each pattern beside the
// reason it is offered — so "the whole class" and "just this file" are told
// apart at a glance rather than by reading the globs.
func (m Model) declarePatternBody() string {
	d := m.diff.declare
	header := []string{
		m.styles.hint.Render("Declare unmergeable — ") + m.styles.unmerge.Render(d.path),
		m.styles.help.Render("git then stops merging it: your version is kept, the conflict is flagged,"),
		m.styles.help.Render("and no conflict markers are written into a file you can't hand-edit."),
		"",
	}

	width := 0
	for _, p := range d.patterns {
		if len(p.pattern) > width {
			width = len(p.pattern)
		}
	}
	var rows []string
	for _, p := range d.patterns {
		rows = append(rows, fmt.Sprintf("%s  %s",
			m.styles.target.Render(padRight(p.pattern, width)), m.styles.help.Render(p.why)))
	}
	return listBody(m.styles, m.width, m.height, header, rows, d.cursor)
}

// declareDestBody asks where it goes, naming each destination's consequence
// rather than its path: the real choice is "the team inherits this" against
// "only this clone does".
func (m Model) declareDestBody() string {
	d := m.diff.declare
	header := []string{
		m.styles.hint.Render("Write ") + m.styles.target.Render(d.pattern+" -merge") +
			m.styles.hint.Render(" to…"),
		"",
	}

	width := 0
	for _, dest := range d.dests {
		if len(dest.Label()) > width {
			width = len(dest.Label())
		}
	}
	var rows []string
	for _, dest := range d.dests {
		rows = append(rows, fmt.Sprintf("%s  %s",
			m.styles.branch.Render(padRight(dest.Label(), width)), m.styles.help.Render(dest.Detail())))
	}
	return listBody(m.styles, m.width, m.height, header, rows, d.cursor)
}

// diffViewportWidth is the inner panel width the diff fills, falling back to a
// sane default before the first WindowSizeMsg.
func diffViewportWidth(s styles, width int) int {
	if w := contentWidth(s, width); w > 0 {
		return w
	}
	return 80
}

// diffViewportHeight is the rows left for the diff after the surrounding chrome
// (header, panel border, the two file-info lines, status, help, padding).
func diffViewportHeight(height int) int {
	const overhead = 9
	if height == 0 {
		return 20
	}
	if h := height - overhead; h > 3 {
		return h
	}
	return 3
}
