package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
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
		files:     st.unmergeable,
		cursor:    0,
		cache:     make(map[string]diffEntry),
		vp:        viewport.New(diffViewportWidth(m.styles, m.width), diffViewportHeight(m.height)),
	}
	m.screen = screenDiff
	m.notice = ""
	return m.loadCurrentDiff()
}

// dispatchDiff runs one named action on the diff panel. Scrolling is not here —
// unbound keys reach the viewport directly (handleKey), so j/k and the page keys
// scroll without a binding.
func (m Model) dispatchDiff(action Action) (tea.Model, tea.Cmd) {
	switch action {
	case ActionCancel:
		m.screen = screenDashboard
		m.diff = diffState{}
		return m, nil
	case ActionNextFile:
		return m.moveDiffFile(1)
	case ActionPrevFile:
		return m.moveDiffFile(-1)
	}
	return m, nil
}

// moveDiffFile steps to another colliding file, clamped at the ends, and shows
// its diff (fetching it the first time).
func (m Model) moveDiffFile(delta int) (tea.Model, tea.Cmd) {
	next := m.diff.cursor + delta
	if next < 0 || next >= len(m.diff.files) {
		return m, nil
	}
	m.diff.cursor = next
	return m.loadCurrentDiff()
}

// loadCurrentDiff shows the selected file's diff. A cached diff renders at once;
// otherwise the panel shows a loading line and a Cmd fetches it off-thread. The
// viewport is reset to the top so each file starts from its first line.
func (m Model) loadCurrentDiff() (Model, tea.Cmd) {
	path := m.diff.files[m.diff.cursor]
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
	if m.diff.files[m.diff.cursor] == msg.path {
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
	return entry.content
}

// diffView renders the panel: which file of how many, the branch→target it
// reconciles, and the scrollable diff itself.
func (m Model) diffView() string {
	d := m.diff
	pos := fmt.Sprintf("%d/%d", d.cursor+1, len(d.files))
	head := m.styles.hint.Render("unmergeable "+pos) + "  " +
		m.styles.target.Render(d.branch+" → "+d.targetKey)
	file := m.styles.unmerge.Render(d.files[d.cursor])

	body := head + "\n" + file + "\n\n" + d.vp.View()
	help := m.styles.help.Render("tab/⇧tab file · j/k scroll · esc back · q quit")
	return m.screenView(body, help)
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
