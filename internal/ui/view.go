package ui

import (
	"fmt"
	"strings"

	"drift/internal/store"
)

// View renders the dashboard. It reads only model state — every git signal was
// computed by a Cmd and folded in by Update, so drawing never blocks.
func (m Model) View() string {
	var b strings.Builder

	b.WriteString(m.header())
	b.WriteString("\n")
	b.WriteString(m.styles.panel.Render(m.body()))
	b.WriteString("\n")
	if line := m.statusLine(); line != "" {
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString(m.help())
	return m.styles.app.Render(b.String())
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
	for i, t := range m.store.Tickets {
		rows = append(rows, m.ticketRow(i, t))
		if m.expanded[t.ID] {
			for _, br := range t.Branches {
				rows = append(rows, m.branchRow(t.ID, br, nameWidth))
			}
			if len(t.Branches) == 0 {
				rows = append(rows, m.styles.hint.Render("    no branches paired"))
			}
		}
	}
	return strings.Join(rows, "\n")
}

func (m Model) emptyState() string {
	lines := []string{
		m.styles.hint.Render("No tickets tracked yet."),
		m.styles.help.Render("Add flow lands next session; until then, seed tickets by"),
		m.styles.help.Render("hand-editing state.json, then press r to refresh."),
	}
	return strings.Join(lines, "\n")
}

// ticketRow renders one ticket headline: expand affordance, ID, optional title.
// The selected row carries a highlighted band.
func (m Model) ticketRow(i int, t store.Ticket) string {
	affordance := "▸"
	if m.expanded[t.ID] {
		affordance = "▾"
	}

	line := affordance + " " + t.ID
	if t.Title != "" {
		line += "  " + t.Title
	}

	if i == m.cursor {
		return m.styles.ticketSel.Render(line)
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
	return m.styles.help.Render(
		"j/k move · enter expand · r refresh · f fetch · a add · d delete · l local · q quit",
	)
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
