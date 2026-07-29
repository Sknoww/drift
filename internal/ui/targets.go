package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// The targets screen: every configured target, and the ref behind it (roadmap
// 19e).
//
// It exists because a target's Ref was write-once and invisible. It is written
// in exactly one place — the first-run wizard — and until now it was *rendered*
// in exactly one, the wizard's own picker row. Every screen the user actually
// lives in shows Target.Key: the dashboard's branch rows, the pairing
// checklist's assignment cell, the status cluster's "unknown target" warning.
// So a target pointing at the wrong branch looked correct everywhere, because
// the key was correct — that is exactly how v0.3.0 published a merge of a
// colleague's ticket branch that happened to be named `…/mvp-3` under a target
// keyed `mvp-3`.
//
// Read-only, deliberately. Showing and correcting are separate jobs (the
// roadmap's own split), and showing is the half that turns an invisible field
// into a visible one. The cursor is here anyway rather than deferred: every
// other list screen has one, and re-pointing a target is an action that hangs
// off a selected row — building the screen cursor-less would make that a
// retrofit rather than an addition, which is the argument area 3 made for named
// actions.
//
// It asks git nothing. The screen's subject is what the *config* says, and a
// probe of whether each ref still resolves would not have caught the incident
// anyway: the wrong ref was a real branch that resolved perfectly.

// openTargets shows the configured targets. No Cmd: the config is already in
// the model, so unlike every other screen there is nothing to load.
func (m Model) openTargets() (tea.Model, tea.Cmd) {
	m.screen = screenTargets
	m.targetsCur = 0
	m.notice = ""
	return m, nil
}

// dispatchTargets runs one named action on the targets screen. Move and back
// out are the whole of it — there is nothing here to choose or commit, so
// Confirm is deliberately unbound rather than made a synonym for anything.
func (m Model) dispatchTargets(action Action) (tea.Model, tea.Cmd) {
	switch action {
	case ActionMoveUp:
		if m.targetsCur > 0 {
			m.targetsCur--
		}
	case ActionMoveDown:
		if m.targetsCur < len(m.cfg.Targets)-1 {
			m.targetsCur++
		}
	case ActionCancel:
		m.screen = screenDashboard
	}
	return m, nil
}

// targetsView is the screen: the list in the shared frame.
func (m Model) targetsView() string {
	help := helpLine(m.styles, m.width,
		[]string{"j/k move"},
		[]string{"esc back", "? help", "q quit"})
	return m.screenView(m.targetsBody(), help)
}

// targetCols is the width budget for a target row: its two variable columns.
type targetCols struct{ key, ref int }

// targetRowFixed is what a target row costs before its variable columns: the
// 2-space indent and the 2-space separator between the key and the ref.
const targetRowFixed = 2 + 2

// targetColumns sizes the row against the panel it has to fit in, in the order
// of allocation every other row here uses (DESIGN.md §1): the cell carrying the
// row's *point* is costed so it can never be squeezed out, and the other absorbs
// what is left.
//
// Which cell that is inverts from the target picker's row, and deliberately. On
// the picker the key is what you are choosing and the ref is the disambiguator;
// here the ref *is* the subject — the whole screen exists because the ref was
// never on screen — so the key column takes its own bounded width first and the
// ref takes everything after it.
//
// A ref longer than what is left ellipsises at its tail, which is the right end
// to lose: `origin/fix/PSOT-22114-…` is the half that gives a wrong target away,
// and the trailing `/mvp-3` is the half that made it look right. A middle-elide
// here would show `origin/fix/…/mvp-3` and hide the one thing worth reading.
func (m Model) targetColumns() targetCols {
	key := m.targetKeyWidth

	cw := rowWidth(m.styles, m.width)
	if cw <= 0 {
		// Size unknown: natural sizing, the same fallback branchColumns takes.
		return targetCols{key: key, ref: widestCell(len(m.cfg.Targets), 0,
			func(i int) string { return m.cfg.Targets[i].Ref })}
	}

	avail := cw - targetRowFixed
	if key > avail-minRefCol {
		if key = avail - minRefCol; key < 0 {
			key = 0
		}
	}
	ref := avail - key
	if ref < 1 {
		ref = 1
	}
	return targetCols{key: key, ref: ref}
}

// targetsBody is the list, headed by what a target is and where it is edited.
//
// The path is on screen because editing is not built yet: hand-editing
// config.json with Drift closed is the correction path today, and it is a route
// nobody finds without reading the source — which is what the v0.3.0 incident
// actually required of its user. It is left to wrap rather than clipped, since
// half a path is a path you cannot act on; headerLines costs a wrapping header
// line at the lines it really takes, so the row budget stays honest (window.go).
func (m Model) targetsBody() string {
	header := []string{
		m.styles.hint.Render("Targets — what your branches are compared against"),
		m.styles.help.Render("edit: " + m.paths.Config),
		"",
	}

	if len(m.cfg.Targets) == 0 {
		return strings.Join(append(header,
			m.styles.hint.Render("No targets configured."),
			m.styles.help.Render("Drift compares nothing until this file names at least one."),
		), "\n")
	}

	cols := m.targetColumns()
	rows := make([]string, len(m.cfg.Targets))
	for i, t := range m.cfg.Targets {
		// The ref carries the row, so it takes the ordinary foreground and the key
		// takes the target style — the reverse weighting of the picker row, where
		// the ref is the small print beside the thing being chosen.
		rows[i] = fmt.Sprintf("  %s  %s",
			m.styles.target.Render(fit(t.Key, cols.key)),
			m.styles.branch.Render(fit(t.Ref, cols.ref)))
	}
	return listBody(m.styles, m.width, m.height, header, rows, m.targetsCur)
}
