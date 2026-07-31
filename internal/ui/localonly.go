package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// The local-only changes manager (roadmap area 6). It is a first-class screen
// rather than a footnote for one reason: **visibility is the whole feature.**
// Raw skip-worktree already holds a tracked file back from every commit — and
// then hides it from `git status` so thoroughly that people forget it exists.
// Drift's contribution is the list.
//
// Everything on screen is derived from Git, never remembered: the held set is
// re-read after every change (loadLocalOnlyCmd), and the store contributes only
// the notes. See docs/specs/local-only-changes.md.

// localOnlyState is the manager's live state. entries is Git's answer, not
// Drift's — rebuilt from ls-files and info/exclude on every load, so a path
// released outside Drift simply stops appearing.
type localOnlyState struct {
	entries []heldPath
	cursor  int
	loaded  bool // false until the first load answers

	add  addLocalState // the "hold a change" picker, open over the list
	note noteState     // the inline note editor, open over the list
}

// heldPath is one path held back from commits, with the primitive holding it.
// tracked is derived from which of Git's two answers the path came from, never
// stored — a file that crosses the tracked/untracked line cannot leave a stale
// label behind.
type heldPath struct {
	path    string
	tracked bool   // held by skip-worktree; false means held by info/exclude
	note    string // from the store, the one thing Drift persists here
}

// mechanism names the primitive doing the holding. Shown per row because it is
// the honest answer to "what did Drift actually do, and what undoes it outside
// Drift" — the same reasoning as the declared badge on the diff panel.
func (h heldPath) mechanism() string {
	if h.tracked {
		return "skip-worktree"
	}
	return "info/exclude"
}

// addLocalState is the candidate picker: Git's working-tree changes, offered so
// the user marks a change they can see rather than typing a path from memory.
// Never auto-suggested and never pre-selected — the project's "never guess" rule.
type addLocalState struct {
	open       bool
	candidates []localCandidate
	cursor     int
	loaded     bool
}

// hasStaged reports whether any candidate is one the screen has to refuse. It is
// what decides whether the header explains staged changes: the explanation is
// worth a line when there is one on the list and is noise when there is not.
func (a addLocalState) hasStaged() bool {
	for _, c := range a.candidates {
		if c.staged {
			return true
		}
	}
	return false
}

// localCandidate is one working-tree change on offer. staged is carried because
// it is the one case a hold cannot honestly serve: skip-worktree hides the
// working tree, not the index, so a staged change would still be committed.
type localCandidate struct {
	path    string
	tracked bool
	staged  bool
}

// noteState is the inline note editor. The note is the only thing Drift stores
// about a hold, and it answers the question the list exists to answer three
// weeks later: why is this here?
type noteState struct {
	open bool
	path string
}

// openLocalOnly enters the manager and asks Git what is held. Nothing is cached
// between visits: the flags can change outside Drift, and the list is only
// worth having if it is true.
func (m Model) openLocalOnly() (tea.Model, tea.Cmd) {
	m.local = localOnlyState{}
	m.screen = screenLocalOnly
	m.notice = ""
	return m, loadLocalOnlyCmd(m.repo)
}

// dispatchLocalOnly runs one named action on the manager, delegating to
// whichever overlay is open over the list.
func (m Model) dispatchLocalOnly(action Action) (tea.Model, tea.Cmd) {
	switch {
	case m.local.note.open:
		return m.dispatchLocalNote(action)
	case m.local.add.open:
		return m.dispatchLocalAdd(action)
	}

	switch action {
	case ActionCancel:
		m.screen = screenDashboard
		m.local = localOnlyState{}
		m.notice = ""
		return m, nil

	case ActionMoveUp:
		if m.local.cursor > 0 {
			m.local.cursor--
		}
		return m, nil

	case ActionMoveDown:
		if m.local.cursor < len(m.local.entries)-1 {
			m.local.cursor++
		}
		return m, nil

	case ActionRefresh:
		return m, loadLocalOnlyCmd(m.repo)

	case ActionHoldLocal:
		return m.beginLocalAdd()

	case ActionRelease:
		return m.releaseSelected()

	case ActionEditNote:
		return m.beginLocalNote()
	}
	return m, nil
}

// selectedHeld is the entry the cursor points at.
func (m Model) selectedHeld() (heldPath, bool) {
	if m.local.cursor < 0 || m.local.cursor >= len(m.local.entries) {
		return heldPath{}, false
	}
	return m.local.entries[m.local.cursor], true
}

// releaseSelected stops holding the selected path, routing to the primitive
// that holds it. Releasing loses nothing: a tracked file's edits reappear at
// once as ordinary working-tree changes, and an untracked file simply shows up
// as untracked again — so it needs no confirmation, only a notice saying what
// just came back.
func (m Model) releaseSelected() (tea.Model, tea.Cmd) {
	h, ok := m.selectedHeld()
	if !ok {
		return m, nil
	}
	m.notice = "releasing " + h.path + "…"
	return m, releaseLocalCmd(m.repo, h.path, h.tracked)
}

// beginLocalAdd opens the candidate picker and asks Git what has changed.
func (m Model) beginLocalAdd() (tea.Model, tea.Cmd) {
	m.local.add = addLocalState{open: true}
	m.notice = ""
	return m, loadLocalCandidatesCmd(m.repo)
}

// dispatchLocalAdd runs one action in the candidate picker — the same
// move/enter/esc shape as every other overlay in Drift.
func (m Model) dispatchLocalAdd(action Action) (tea.Model, tea.Cmd) {
	switch action {
	case ActionCancel:
		m.local.add = addLocalState{}
		return m, nil

	case ActionMoveUp:
		if m.local.add.cursor > 0 {
			m.local.add.cursor--
		}
		return m, nil

	case ActionMoveDown:
		if m.local.add.cursor < len(m.local.add.candidates)-1 {
			m.local.add.cursor++
		}
		return m, nil

	case ActionConfirm:
		return m.holdSelectedCandidate()
	}
	return m, nil
}

// holdSelectedCandidate holds the chosen change, routed by whether Git tracks
// it. A staged change is refused rather than half-held: skip-worktree hides the
// working tree from Git, but the index is what a commit writes, so holding a
// staged change would look like protection and give none.
func (m Model) holdSelectedCandidate() (tea.Model, tea.Cmd) {
	a := m.local.add
	if a.cursor < 0 || a.cursor >= len(a.candidates) {
		return m, nil
	}
	c := a.candidates[a.cursor]
	if c.staged {
		m.notice = "unstage " + c.path + " first (git restore --staged) — a hold can't cover the index"
		return m, nil
	}

	m.local.add = addLocalState{}
	m.notice = "holding " + c.path + "…"
	return m, holdLocalCmd(m.repo, c.path, c.tracked)
}

// beginLocalNote opens the note editor for the selected entry, seeded with
// whatever note it already carries.
func (m Model) beginLocalNote() (tea.Model, tea.Cmd) {
	h, ok := m.selectedHeld()
	if !ok {
		return m, nil
	}
	ti := textinput.New()
	ti.Placeholder = "why it's held"
	ti.CharLimit = 120
	ti.SetValue(h.note)
	ti.CursorEnd()
	ti.Focus()

	m.input = ti
	m.local.note = noteState{open: true, path: h.path}
	m.notice = ""
	return m, textinput.Blink
}

// dispatchLocalNote handles the note editor. Only Confirm/Cancel/Quit reach
// here; every other keystroke is fed to the text input by handleKey.
func (m Model) dispatchLocalNote(action Action) (tea.Model, tea.Cmd) {
	switch action {
	case ActionCancel:
		m.local.note = noteState{}
		return m, nil

	case ActionConfirm:
		path := m.local.note.path
		note := strings.TrimSpace(m.input.Value())
		m.local.note = noteState{}
		m.store = m.store.SetLocalOnlyNote(path, note)
		for i := range m.local.entries {
			if m.local.entries[i].path == path {
				m.local.entries[i].note = note
			}
		}
		return m, saveStateCmd(m.repo, m.store)
	}
	return m, nil
}

// applyLocalOnly folds Git's answer into the manager, joins each held path to
// its stored note, and drops the notes for paths Git no longer holds.
//
// That prune is the store's whole contract here: it persists annotations and
// nothing else, so it can never claim a hold Git does not have. A path released
// outside Drift loses its note on the next load rather than lingering as a
// stale claim.
func (m Model) applyLocalOnly(msg localOnlyMsg) (Model, tea.Cmd) {
	if m.screen != screenLocalOnly {
		return m, nil
	}
	m.local.loaded = true
	if msg.err != nil {
		m.notice = "couldn't read what's held: " + msg.err.Error()
		return m, nil
	}

	entries := msg.held
	for i := range entries {
		entries[i].note = m.store.LocalOnlyNote(entries[i].path)
	}
	m.local.entries = entries
	if m.local.cursor >= len(entries) {
		m.local.cursor = len(entries) - 1
	}
	if m.local.cursor < 0 {
		m.local.cursor = 0
	}

	store, pruned := m.store.PruneLocalOnly(heldPaths(entries))
	if !pruned {
		return m, nil
	}
	m.store = store
	return m, saveStateCmd(m.repo, m.store)
}

// applyLocalHold reports a completed hold or release, then re-reads the held
// set from Git rather than assuming what Drift's own write achieved — the same
// rule the declare flow follows, and for the same reason: the list is only
// worth having if it is Git's answer.
func (m Model) applyLocalHold(msg localHoldMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		verb := "release"
		if msg.hold {
			verb = "hold"
		}
		m.notice = "couldn't " + verb + " " + msg.path + ": " + msg.err.Error()
		return m, nil
	}
	if msg.hold {
		m.notice = "holding " + msg.path + " — it stays out of every commit"
	} else {
		m.notice = "released " + msg.path + " — it's an ordinary change again"
	}
	return m, loadLocalOnlyCmd(m.repo)
}

// applyLocalCandidates folds the working-tree scan into the picker. Held paths
// are filtered out: a skip-worktree file reads as unmodified and an excluded
// file is ignored, so Git rarely offers one anyway — but a staged edit to an
// already-held path can still surface, and offering "hold this" for something
// already held would be a lie.
func (m Model) applyLocalCandidates(msg localCandidatesMsg) Model {
	if m.screen != screenLocalOnly || !m.local.add.open {
		return m
	}
	m.local.add.loaded = true
	if msg.err != nil {
		m.notice = "couldn't list working-tree changes: " + msg.err.Error()
		return m
	}

	held := make(map[string]bool, len(m.local.entries))
	for _, h := range m.local.entries {
		held[h.path] = true
	}
	var cands []localCandidate
	for _, c := range msg.changes {
		if held[c.Path] {
			continue
		}
		cands = append(cands, localCandidate{path: c.Path, tracked: c.Tracked, staged: c.Staged})
	}
	m.local.add.candidates = cands
	m.local.add.cursor = 0
	return m
}

// heldPaths flattens the entries for a call that only needs the names.
func heldPaths(entries []heldPath) []string {
	out := make([]string, len(entries))
	for i, h := range entries {
		out[i] = h.path
	}
	return out
}

// localOnlyView draws the manager, or whichever overlay is open in its place —
// the same mechanism as the target picker and the declare overlay.
func (m Model) localOnlyView() string {
	switch {
	case m.local.note.open:
		return m.screenView(m.localNoteBody(),
			helpLine(m.styles, m.width, nil, []string{"enter save", "esc cancel"}))
	case m.local.add.open:
		return m.screenView(m.localAddBody(),
			helpLine(m.styles, m.width, []string{"j/k move"}, []string{"enter hold", "esc back"}))
	}
	help := helpLine(m.styles, m.width,
		[]string{"j/k move", "a hold", "d release", "n note", "r refresh"},
		[]string{"esc back", "? help", "q quit"})
	return m.screenView(m.localOnlyBody(), help)
}

// localOnlyBody lists what is held. The second header line states the scope
// outright: a hold is an index or ignore flag, so it applies to every branch the
// user checks out. The UI must not imply otherwise (CONTEXT.md), and a list of
// paths with no scope stated would.
func (m Model) localOnlyBody() string {
	header := []string{
		m.styles.hint.Render("Local-only changes — kept on this machine, never committed"),
		m.styles.help.Render("Held on every branch you check out: these are git index and ignore flags, not per-branch."),
		"",
	}

	if !m.local.loaded {
		return strings.Join(append(header, m.styles.help.Render("reading what's held…")), "\n")
	}
	if len(m.local.entries) == 0 {
		return strings.Join(append(header,
			m.styles.hint.Render("Nothing held yet."),
			m.styles.help.Render("Press a to hold a working-tree change — a log tweak, a local override, a scratch file."),
		), "\n")
	}

	// The mechanism column takes its content's width: two literals, neither of
	// them user-supplied. The path column is the opposite — it is whatever the
	// repo holds — so it is squeezed against the note's floor, and takes its own
	// content's width whenever the panel has the room (roadmap area 20).
	mechWidth := widestCell(len(m.local.entries), func(i int) string { return m.local.entries[i].mechanism() })
	pathWidth := widestCell(len(m.local.entries), func(i int) string { return m.local.entries[i].path })
	if cw := rowWidth(m.styles, m.width); cw > 0 {
		const fixed = 1 + 1 + 2 + 2 // glyph and the three separators
		if avail := cw - fixed - mechWidth - minNameCol; pathWidth > avail {
			if pathWidth = avail; pathWidth < minNameCol {
				pathWidth = minNameCol
			}
		}
	}
	var rows []string
	for _, h := range m.local.entries {
		rows = append(rows, fmt.Sprintf("%s %s  %s  %s",
			m.heldGlyph(h),
			m.styles.branch.Render(fit(h.path, pathWidth)),
			m.styles.help.Render(fit(h.mechanism(), mechWidth)),
			m.styles.target.Render(h.note)))
	}
	detail := ""
	if c := m.local.cursor; c >= 0 && c < len(m.local.entries) {
		detail = m.local.entries[c].path
	}
	return listBody(m.styles, m.width, m.height, header, rows, m.local.cursor, detail)
}

// heldGlyph marks which primitive holds a path. The tracked one is filled and
// colored because it is the one that needs to be unmistakable: Git hides a
// skip-worktree file from `git status` entirely, so this list is the only place
// it is visible at all (docs/specs/local-only-changes.md).
func (m Model) heldGlyph(h heldPath) string {
	if h.tracked {
		return m.styles.dirty.Render("◆")
	}
	return m.styles.help.Render("◇")
}

// localAddBody offers the working-tree changes. Each shows the primitive that
// would hold it, so the routing is visible rather than magic — the user picks a
// change, and Drift says out loud what it will do about it.
func (m Model) localAddBody() string {
	header := []string{
		m.styles.hint.Render("Hold a change locally"),
		m.styles.help.Render("It stays in your working tree on every branch, and never reaches a commit."),
	}
	// The reason a staged change is refused is the same reason on every row that
	// carries one, so it is stated once here rather than paid for in the detail
	// column on every row — see candidateDetail. Only shown when there is one to
	// explain: a header line about a case the list does not contain is noise.
	if m.local.add.hasStaged() {
		header = append(header,
			m.styles.help.Render("A staged change can't be held — a hold can't cover the index."))
	}
	header = append(header, "")

	if !m.local.add.loaded {
		return strings.Join(append(header, m.styles.help.Render("scanning the working tree…")), "\n")
	}
	if len(m.local.add.candidates) == 0 {
		return strings.Join(append(header,
			m.styles.help.Render("No working-tree change to hold — everything is committed or already held."),
		), "\n")
	}

	// The detail cell is what this screen is for — it names the primitive, or
	// refuses a staged change — so it is costed first and the path takes what is
	// left, the same ordering the pairing checklist uses.
	//
	// **Reserve what the row spends, and spend what is reserved.** This did
	// neither: it took the widest detail out of the path's budget and then
	// rendered each detail unpadded, so a `tracked → skip-worktree` row bought
	// alignment nothing and cost the path thirty cells — with the caps gone, that
	// reservation was what still held the path column to 45 at 110 columns
	// (roadmap area 20). The refusal was shortened to the width of the two it sits
	// beside, and the details are padded to the column that pays for them.
	details := make([]string, len(m.local.add.candidates))
	for i, c := range m.local.add.candidates {
		details[i] = m.candidateDetail(c)
	}
	detailWidth := widestCell(len(details), func(i int) string { return details[i] })
	width := widestCell(len(m.local.add.candidates),
		func(i int) string { return m.local.add.candidates[i].path })
	if cw := rowWidth(m.styles, m.width); cw > 0 {
		if avail := cw - 2 - detailWidth; width > avail {
			if width = avail; width < minNameCol {
				width = minNameCol
			}
		}
	}
	var rows []string
	for i, c := range m.local.add.candidates {
		rows = append(rows, fmt.Sprintf("%s  %s",
			m.styles.branch.Render(fit(c.path, width)), fit(details[i], detailWidth)))
	}
	// The screen the whole area was raised from: seven consecutive rows reading
	// `main-connector/src/main/java/com/teamviewer/con…`. The path is the value.
	detail := ""
	if c := m.local.add.cursor; c >= 0 && c < len(m.local.add.candidates) {
		detail = m.local.add.candidates[c].path
	}
	return listBody(m.styles, m.width, m.height, header, rows, m.local.add.cursor, detail)
}

// candidateDetail says what holding this change would do — or why it can't.
//
// The refusal states what is wrong and how to fix it, and stops there. *Why* a
// hold cannot cover the index is the same sentence on every staged row, so it is
// in the screen's header — a column is sized by its longest cell, and a
// per-row explanation of a per-screen fact was being paid for by every path on
// the list (roadmap area 20).
func (m Model) candidateDetail(c localCandidate) string {
	if c.staged {
		return m.styles.errText.Render("staged — unstage it first")
	}
	if c.tracked {
		return m.styles.help.Render("tracked   → skip-worktree")
	}
	return m.styles.help.Render("untracked → info/exclude")
}

// localNoteBody is the inline note editor. The note is for the reader three
// weeks from now, which is the whole reason the store persists anything here.
func (m Model) localNoteBody() string {
	lines := []string{
		m.styles.hint.Render("Note for ") + m.styles.branch.Render(m.local.note.path),
		m.styles.help.Render("Why it's held — so future you doesn't have to work it out. Empty clears it."),
		"",
		"  " + m.input.View(),
	}
	return strings.Join(lines, "\n")
}
