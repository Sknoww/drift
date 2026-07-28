package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Sknoww/drift/internal/store"
)

// View renders the current screen. It reads only model state — every git signal
// was computed by a Cmd and folded in by Update, so drawing never blocks.
func (m Model) View() string {
	if m.showHelp {
		return m.helpView()
	}
	switch m.screen {
	case screenAddID:
		return m.addIDView()
	case screenPairing:
		return m.pairingView()
	case screenDiff:
		return m.diffView()
	case screenLocalOnly:
		return m.localOnlyView()
	case screenShelve:
		return m.shelveView()
	default: // dashboard, and the delete confirmation drawn over it
		return m.dashboardView()
	}
}

// screenView is the shared frame — header, a bordered panel, an optional status
// line, and a help line — so every screen sits in the same chrome. The panel
// spans the full terminal width so every screen (and the area-5 diff panel to
// come) shares one full-width layout rather than a snug content-sized box.
func (m Model) screenView(panel, help string) string {
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n")
	b.WriteString(panelStyle(m.styles, m.width).Render(panel))
	b.WriteString("\n")
	if line := m.statusLine(); line != "" {
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString(help)
	return m.styles.app.Render(b.String())
}

// dashboardView is the ticket list — the home screen, and the backdrop the
// delete confirmation is drawn into.
func (m Model) dashboardView() string {
	return m.screenView(m.body(), m.help())
}

// header is the title row, with a spinner while a sweep is in flight and the
// checked-out branch for orientation.
func (m Model) header() string {
	title := m.styles.title.Render("drift")

	right := ""
	switch {
	case m.loading:
		right = m.styles.help.Render(m.spin.View() + "refreshing")
	case m.current != "":
		marker := m.styles.marker.Render("▸ ")
		right = marker + m.styles.help.Render("on "+m.current)
	case m.current == "":
		right = m.styles.help.Render("(detached HEAD)")
	}
	return title + "  " + right
}

// body is the ticket list, or the empty-state teach line when nothing is tracked.
func (m Model) body() string {
	if len(m.store.Tickets) == 0 {
		return m.emptyState()
	}

	nameWidth := m.branchNameWidth()
	sel, selOK := m.selectedRow() // the semantic cursor row

	var rows []string
	selected := -1
	for ti, t := range m.store.Tickets {
		if selOK && !sel.isBranch() && sel.ticket == ti {
			selected = len(rows) // the ticket headline is the selected row
		}
		rows = append(rows, m.ticketRow(t))

		// The delete confirmation is an inline prompt under its ticket, so the
		// user sees exactly what is about to go.
		if m.screen == screenConfirmDelete && t.ID == m.pendingDelete {
			rows = append(rows, m.styles.errText.Render(
				fmt.Sprintf("  delete %s and its %s?  (y/n)", t.ID, plural(len(t.Branches), "pairing"))))
			continue
		}

		if m.expanded[t.ID] {
			for bi, br := range t.Branches {
				if selOK && sel.isBranch() && sel.ticket == ti && sel.branch == bi {
					selected = len(rows) // a branch row is selectable in its own right
				}
				rows = append(rows, m.branchRow(t.ID, br, nameWidth))
			}
			if len(t.Branches) == 0 {
				rows = append(rows, m.styles.hint.Render("    no branches paired"))
			}
		}
	}
	return listBody(m.styles, m.width, m.height, nil, rows, selected)
}

// panelStyle is the bordered panel sized to span the terminal.
//
// Lip Gloss counts a style's own horizontal padding inside Width() but not its
// border, so the panel must be set to contentWidth *plus* its padding. Setting
// it to contentWidth alone leaves a text area two cells narrower than the rows
// built to fill it, and every full-width row — the selection band above all —
// wraps by exactly that padding, dropping its tail onto the next line.
//
// A free function on (styles, width) so the dashboard, the diff panel, and the
// first-run wizard share one implementation of the full-width frame.
func panelStyle(s styles, width int) lipgloss.Style {
	cw := contentWidth(s, width)
	if cw <= 0 {
		return s.panel // size still unknown: fall back to natural content sizing
	}
	return s.panel.Width(cw + s.panel.GetHorizontalPadding())
}

// contentWidth is the panel's inner content width: the terminal width less the
// app padding and the panel's border+padding. It is the span a full-width panel
// and its selection band fill. Returns 0 before the first WindowSizeMsg (size
// still unknown), signalling callers to fall back to natural content sizing.
//
// It is a free function on (styles, width) rather than a method so the dashboard
// and the first-run wizard (wizard.go) share one implementation of the
// full-width frame.
func contentWidth(s styles, width int) int {
	if width == 0 {
		return 0
	}
	w := width - s.app.GetHorizontalFrameSize() - s.panel.GetHorizontalFrameSize()
	if w < 1 {
		return 1
	}
	return w
}

// selectBand highlights rows[selected] as a band filling the panel's full inner
// width, so the selection reads as a row rather than hugging its text
// (DESIGN.md §3). Before the size is known it falls back to the widest row. A
// selected index outside the slice leaves every row untouched — screens with no
// active selection pass -1. Shared by the dashboard and the wizard, hence a free
// function on (styles, width).
func selectBand(s styles, width int, rows []string, selected int) []string {
	if selected < 0 || selected >= len(rows) {
		return rows
	}
	w := 0
	for _, r := range rows {
		if lw := lipgloss.Width(r); lw > w {
			w = lw
		}
	}
	if cw := contentWidth(s, width); cw > w {
		w = cw // fill the whole panel, not just up to the widest row
	}
	out := make([]string, len(rows))
	copy(out, rows)
	out[selected] = s.ticketSel.Width(w).Render(reopenBand(s, rows[selected]))
	return out
}

// reopenBand re-opens the band's own escape sequence after every reset inside a
// row.
//
// A row is assembled from independently styled cells — branch name, target,
// ↓behind ↑ahead, the dirty dot, the unmergeable marker — and each of those ends
// with a *full* SGR reset, which switches the band's background back off partway
// along the line. Wrapping such a row in a background style therefore paints
// only as far as the first inner reset, plus the trailing pad the outer style
// adds itself: a highlight that covers the branch name, skips the middle of the
// row, and reappears at the right-hand edge.
//
// The sequence is discovered by rendering a sentinel through the band style
// rather than hardcoded, so it follows whatever color profile the terminal
// actually got. On a profile with no color it comes back empty and this is a
// no-op — which is also why a test can't see the bug this fixes.
func reopenBand(s styles, row string) string {
	const sentinel = "\x00"
	open, _, found := strings.Cut(s.ticketSel.Render(sentinel), sentinel)
	if !found {
		return row
	}
	return reopenAfterResets(row, open)
}

// reopenAfterResets re-arms open after every reset in row. Split out from
// reopenBand so it is testable: a test's color profile makes the band's own
// sequence empty, and this half can still be driven with a real one.
func reopenAfterResets(row, open string) string {
	if open == "" {
		return row // no color on this terminal: nothing to re-arm
	}
	return strings.ReplaceAll(row, ansiReset, ansiReset+open)
}

// ansiReset is the sequence Lip Gloss closes every styled span with.
const ansiReset = "\x1b[0m"

func (m Model) emptyState() string {
	lines := []string{
		m.styles.hint.Render("No tickets tracked yet."),
		m.styles.help.Render("Press a to add one — Drift matches your branches to it."),
	}
	return strings.Join(lines, "\n")
}

// ticketRow renders one ticket headline: expand affordance, ID, optional title.
// The selection band is applied by the caller (selectBand) once every row's
// width is known, so this only produces the row's content.
func (m Model) ticketRow(t store.Ticket) string {
	affordance := "▸"
	if m.expanded[t.ID] {
		affordance = "▾"
	}

	line := affordance + " " + t.ID
	if t.Title != "" {
		line += "  " + t.Title
	}
	return m.styles.ticket.Render(line)
}

// branchNameWidth is the widest branch name across every ticket, capped so a
// single long name can't shove the status cluster off-screen. Computing it over
// all tickets (not just expanded ones) keeps the status columns aligned down the
// whole dashboard as tickets expand and collapse (DESIGN.md §1).
func (m Model) branchNameWidth() int {
	const cap = 32
	w := 0
	for _, t := range m.store.Tickets {
		for _, br := range t.Branches {
			if len(br.Branch) > w {
				w = len(br.Branch)
			}
		}
	}
	if w > cap {
		return cap
	}
	return w
}

// branchRow renders one paired branch beneath its ticket, with the status
// cluster in the fixed order from DESIGN.md §1: target · ↓behind ↑ahead ·
// dirty · checked-out marker, then the area-5 unmergeable marker at the end so
// it never disturbs the aligned columns.
func (m Model) branchRow(ticketID string, br store.TicketBranch, nameWidth int) string {
	st := m.status[statusKey(ticketID, br.Branch)]

	name := m.styles.branch.Render("    " + padRight(br.Branch, nameWidth))
	target := m.styles.target.Render(padRight(br.TargetKey, m.targetKeyWidth))
	ab := m.renderAheadBehind(br, st)

	dirty := "  "
	if br.Branch == m.current && m.dirty {
		dirty = m.styles.dirty.Render("● ")
	}

	marker := "  "
	if br.Branch == m.current {
		marker = m.styles.marker.Render("▸ ")
	}

	row := fmt.Sprintf("%s   %s   %s  %s%s", name, target, ab, dirty, marker)

	// An unmergeable collision is the signal this whole area exists to surface:
	// the target moved under a file that must be reconciled by hand. Flag it with
	// a count; enter on the row opens the diff. Trailing so alignment is untouched.
	if n := len(st.unmergeable); n > 0 {
		row += "  " + m.styles.unmerge.Render(fmt.Sprintf("⚠ %d unmergeable", n))
	}
	return row
}

// renderAheadBehind draws the ↓behind ↑ahead pair, or the reason it is absent:
// an unknown target key (stale pairing) or a probe error (e.g. the branch is
// gone locally). behind>0 is the one value that reads as a warning.
func (m Model) renderAheadBehind(br store.TicketBranch, st branchStatus) string {
	if !st.known {
		return m.styles.errText.Render("⚠ unknown target " + quote(br.TargetKey))
	}
	if st.err != nil {
		return m.styles.errText.Render("⚠ no status")
	}
	if st.behind == 0 && st.ahead == 0 {
		return m.styles.sync.Render("↓0 ↑0")
	}

	behind := m.styles.sync.Render(fmt.Sprintf("↓%d", st.behind))
	if st.behind > 0 {
		behind = m.styles.behind.Render(fmt.Sprintf("↓%d", st.behind))
	}
	ahead := m.styles.ahead.Render(fmt.Sprintf("↑%d", st.ahead))
	return behind + " " + ahead
}

// statusLine surfaces the last error or transient notice under the panel.
func (m Model) statusLine() string {
	if m.err != nil {
		return m.styles.errText.Render("error: " + m.err.Error())
	}
	if m.notice != "" {
		return m.styles.hint.Render(m.notice)
	}
	return ""
}

func (m Model) help() string {
	if m.screen == screenConfirmDelete {
		return m.styles.help.Render("y confirm · n cancel")
	}
	return m.styles.help.Render(
		"j/k move · enter expand/diff · s shelve · r refresh · f fetch · a add · d delete · l local · ? help · q quit",
	)
}

// addIDView is the ticket-ID entry screen.
func (m Model) addIDView() string {
	panel := m.styles.hint.Render("New ticket") + "\n\n" +
		"  ID  " + m.input.View()
	help := m.styles.help.Render("enter continue · esc cancel")
	return m.screenView(panel, help)
}

// pairingView is the candidate checklist, or the target picker overlay drawn in
// its place while it is open.
func (m Model) pairingView() string {
	if m.add.picker {
		help := m.styles.help.Render("j/k move · enter select · esc back")
		return m.screenView(m.pickerBody(), help)
	}
	help := m.styles.help.Render("space toggle · t target · 1–9 quick-target · enter save · esc cancel")
	return m.screenView(m.pairingBody(), help)
}

// pairingBody lists the candidate branches with their inclusion box and
// assigned target. An included branch with no target is flagged, never guessed.
func (m Model) pairingBody() string {
	header := []string{m.styles.hint.Render("Pair branches for " + m.add.id)}

	if !m.add.loaded {
		return strings.Join(append(header, "", m.styles.help.Render("scanning branches…")), "\n")
	}
	if len(m.add.candidates) == 0 {
		return strings.Join(append(header, "",
			m.styles.help.Render("No local branch contains "+quote(m.add.id)+"."),
			m.styles.help.Render("enter saves the ticket with no branches; pair them later.")), "\n")
	}

	header = append(header, "")
	nameWidth := m.candidateNameWidth()
	var rows []string
	for _, c := range m.add.candidates {
		box := "[ ]"
		if c.included {
			box = "[x]"
		}

		assign := ""
		if c.included {
			if c.targetKey == "" {
				assign = m.styles.errText.Render("⚠ pick a target")
			} else {
				assign = m.styles.target.Render("→ " + c.targetKey)
			}
		}

		rows = append(rows, fmt.Sprintf("%s %s  %s", box, padRight(c.branch, nameWidth), assign))
	}
	return listBody(m.styles, m.width, m.height, header, rows, m.add.cursor)
}

// pickerBody lists every configured target for the selected candidate, showing
// Key and Ref so a terse key is never ambiguous. The 1–9 accelerators are shown
// against the first nine; the rest are reachable by moving the cursor.
func (m Model) pickerBody() string {
	cand := m.add.candidates[m.add.cursor].branch
	header := []string{m.styles.hint.Render("Target for " + cand), ""}

	var rows []string
	for i, t := range m.cfg.Targets {
		acc := "  "
		if i < 9 {
			acc = fmt.Sprintf("%d ", i+1)
		}
		rows = append(rows, fmt.Sprintf("%s %s  %s",
			m.styles.help.Render(acc), padRight(t.Key, m.targetKeyWidth), m.styles.help.Render(t.Ref)))
	}
	return listBody(m.styles, m.width, m.height, header, rows, m.add.pickerCur)
}

// candidateNameWidth is the widest candidate branch name, capped so a single
// long name cannot shove the target column off-screen (mirrors branchNameWidth).
func (m Model) candidateNameWidth() int {
	const cap = 32
	w := 0
	for _, c := range m.add.candidates {
		if len(c.branch) > w {
			w = len(c.branch)
		}
	}
	if w > cap {
		return cap
	}
	return w
}

func padRight(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}

func quote(s string) string {
	return "\"" + s + "\""
}

// plural renders a count with its noun, adding an "s" for anything but one.
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
