package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"drift/internal/store"
)

// View renders the current screen. It reads only model state — every git signal
// was computed by a Cmd and folded in by Update, so drawing never blocks.
func (m Model) View() string {
	switch m.screen {
	case screenAddID:
		return m.addIDView()
	case screenPairing:
		return m.pairingView()
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
	panelStyle := m.styles.panel
	if cw := m.contentWidth(); cw > 0 {
		panelStyle = panelStyle.Width(cw)
	}
	b.WriteString(panelStyle.Render(panel))
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

	var rows []string
	selected := -1
	for i, t := range m.store.Tickets {
		if i == m.cursor {
			selected = len(rows) // the ticket headline is the selectable row
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
			for _, br := range t.Branches {
				rows = append(rows, m.branchRow(t.ID, br, nameWidth))
			}
			if len(t.Branches) == 0 {
				rows = append(rows, m.styles.hint.Render("    no branches paired"))
			}
		}
	}
	return strings.Join(m.selectBand(rows, selected), "\n")
}

// contentWidth is the panel's inner content width: the terminal width less the
// app padding and the panel's border+padding. It is the span a full-width panel
// and its selection band fill. Returns 0 before the first WindowSizeMsg (size
// still unknown), signalling callers to fall back to natural content sizing.
func (m Model) contentWidth() int {
	if m.width == 0 {
		return 0
	}
	w := m.width - m.styles.app.GetHorizontalFrameSize() - m.styles.panel.GetHorizontalFrameSize()
	if w < 1 {
		return 1
	}
	return w
}

// selectBand highlights rows[selected] as a band filling the panel's full inner
// width, so the selection reads as a row rather than hugging its text
// (DESIGN.md §3). Before the size is known it falls back to the widest row. A
// selected index outside the slice leaves every row untouched — screens with no
// active selection pass -1.
func (m Model) selectBand(rows []string, selected int) []string {
	if selected < 0 || selected >= len(rows) {
		return rows
	}
	w := 0
	for _, r := range rows {
		if lw := lipgloss.Width(r); lw > w {
			w = lw
		}
	}
	if cw := m.contentWidth(); cw > w {
		w = cw // fill the whole panel, not just up to the widest row
	}
	out := make([]string, len(rows))
	copy(out, rows)
	out[selected] = m.styles.ticketSel.Width(w).Render(rows[selected])
	return out
}

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
// dirty · checked-out marker.
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

	return fmt.Sprintf("%s   %s   %s  %s%s", name, target, ab, dirty, marker)
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
		"j/k move · enter expand · r refresh · f fetch · a add · d delete · l local · q quit",
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
	lines := []string{m.styles.hint.Render("Pair branches for " + m.add.id)}

	if !m.add.loaded {
		return strings.Join(append(lines, "", m.styles.help.Render("scanning branches…")), "\n")
	}
	if len(m.add.candidates) == 0 {
		lines = append(lines, "",
			m.styles.help.Render("No local branch contains "+quote(m.add.id)+"."),
			m.styles.help.Render("enter saves the ticket with no branches; pair them later."))
		return strings.Join(lines, "\n")
	}

	lines = append(lines, "")
	head := len(lines) // first candidate row; the cursor indexes from here
	nameWidth := m.candidateNameWidth()
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

		lines = append(lines, fmt.Sprintf("%s %s  %s", box, padRight(c.branch, nameWidth), assign))
	}
	return strings.Join(m.selectBand(lines, head+m.add.cursor), "\n")
}

// pickerBody lists every configured target for the selected candidate, showing
// Key and Ref so a terse key is never ambiguous. The 1–9 accelerators are shown
// against the first nine; the rest are reachable by moving the cursor.
func (m Model) pickerBody() string {
	cand := m.add.candidates[m.add.cursor].branch
	lines := []string{m.styles.hint.Render("Target for " + cand), ""}

	head := len(lines) // first target row; pickerCur indexes from here
	for i, t := range m.cfg.Targets {
		acc := "  "
		if i < 9 {
			acc = fmt.Sprintf("%d ", i+1)
		}
		lines = append(lines, fmt.Sprintf("%s %s  %s",
			m.styles.help.Render(acc), padRight(t.Key, m.targetKeyWidth), m.styles.help.Render(t.Ref)))
	}
	return strings.Join(m.selectBand(lines, head+m.add.pickerCur), "\n")
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
