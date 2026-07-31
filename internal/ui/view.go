package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

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
	case screenTargets:
		return m.targetsView()
	default: // dashboard, and the delete confirmation drawn over it
		return m.dashboardView()
	}
}

// screenView is the shared frame — header, a bordered panel, an optional status
// line, and a help line — so every screen sits in the same chrome. The panel
// spans the full terminal width so every screen (and the area-5 diff panel to
// come) shares one full-width layout rather than a snug content-sized box.
func (m Model) screenView(panel, help string) string {
	if v, ok := tooNarrowView(m.styles, m.width); ok {
		return m.styles.app.Render(v)
	}
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
//
// The re-pair picker (19b) is drawn in the panel's place while it is open, the
// same as the target picker over the pairing checklist. The ticket list is not
// visible behind it, which is why the picker's header names the branch: an overlay
// has to say what it is about, since the row it is about is what it covered.
func (m Model) dashboardView() string {
	if m.repair.open {
		help := helpLine(m.styles, m.width,
			[]string{"j/k move", "1–9 quick-target"}, []string{"enter re-pair", "esc back"})
		return m.screenView(m.targetPickerBody(m.repair.branch, m.repair.from, m.repair.cursor), help)
	}
	return m.screenView(m.body(), m.help())
}

// header is the title row, with a spinner while a sweep is in flight and the
// checked-out branch for orientation.
//
// The right-hand cell carries a branch name — repo-supplied and unbounded, like
// any name on a row below it — so it absorbs what the title leaves rather than
// wrapping the header onto a second line the frame never budgeted for. Same
// order of allocation as a branch row: the fixed cost is paid first (chrome.go).
func (m Model) header() string {
	title := titleText(m.styles)

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

	if cw := chromeWidth(m.styles, m.width); cw > 0 {
		// The cut is ANSI-aware: right is assembled from styled cells, and
		// slicing one by bytes would sever an escape sequence (clipRow, §1).
		avail := cw - lipgloss.Width(title) - 2
		if avail < 1 {
			return ansi.Truncate(title, cw, helpElide)
		}
		right = ansi.Truncate(right, avail, helpElide)
	}
	return title + "  " + right
}

// body is the ticket list, or the empty-state teach line when nothing is tracked.
//
// The detail line's value is the awkward one on this screen, because the cursor
// moves between two kinds of row: a branch row's value is its branch name, a
// ticket row's is the headline text. It is picked where the cursor is found
// rather than re-derived afterwards — the walk already knows which kind it
// landed on, and asking twice is how the two answers drift apart.
func (m Model) body() string {
	if len(m.store.Tickets) == 0 {
		return m.emptyState()
	}

	cols := m.branchColumns()
	sel, selOK := m.selectedRow() // the semantic cursor row

	var rows []string
	selected, detail := -1, ""
	for ti, t := range m.store.Tickets {
		if selOK && !sel.isBranch() && sel.ticket == ti {
			selected = len(rows) // the ticket headline is the selected row
			detail = ticketText(t)
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
					detail = br.Branch
				}
				rows = append(rows, m.branchRow(t.ID, br, cols))
			}
			if len(t.Branches) == 0 {
				rows = append(rows, m.styles.hint.Render("    no branches paired"))
			}
		}
	}
	return listBody(m.styles, m.width, m.height, nil, rows, selected, detail)
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
//
// The clamp to 1 is a guard, not a policy: a terminal narrow enough to reach it
// is under minTerminalWidth and is never drawn at all (tooNarrowView). Declaring
// the floor is what the clamp used to be doing badly — rendering garbage into a
// width it could not fill rather than saying so.
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

// rowWidth is what a *row* has to spend, as against contentWidth, which is what
// the *panel* spans. They differ by the selection gutter: a marker treatment
// reserves cells at the left edge of every row for its glyph, and those cells
// are not the row's to fill (band.go).
//
// Every column budget measures against this one; the panel, the band and the
// chrome keep measuring against contentWidth. Sizing rows against the panel and
// then prefixing a marker is the bug this exists to avoid — the row overflows by
// exactly the gutter, and clipRow cuts the trailing status cluster, so the
// treatment appears to cost a signal it does not cost.
//
// With no gutter — every band treatment, and so every run today — it is
// contentWidth exactly.
func rowWidth(s styles, width int) int {
	cw := contentWidth(s, width)
	if cw <= 0 {
		return cw // size unknown: callers fall back to natural sizing
	}
	if w := cw - s.band.gutter(); w > 0 {
		return w
	}
	return 1
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
	if !s.band.fill {
		return rows // a marker-only treatment: the row is already marked, nothing to paint
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

	return m.styles.ticket.Render(affordance + " " + ticketText(t))
}

// ticketText is the headline a ticket row carries, without the expand
// affordance in front of it: the ID, and the title when there is one.
//
// It is shared with the detail line rather than reassembled there, so the two
// cannot say different things about the same ticket — the detail is only ever
// drawn when it is *not* a substring of the drawn row, and two spellings of the
// headline would make that test answer for a string the screen never held.
func ticketText(t store.Ticket) string {
	if t.Title == "" {
		return t.ID
	}
	return t.ID + "  " + t.Title
}

// branchCols is the width budget for the dashboard's branch rows: the two
// variable columns, plus the width the status pair is aligned to.
type branchCols struct{ name, target, ab int }

// branchRowFixed is what a branch row costs before its variable columns: the
// 4-space indent, the three separators ("   ", "   ", "  ") between name,
// target, the status pair and the glyph cluster, and the three 2-cell cells
// that close it (publish, dirty, checked-out marker). It is the constant in the
// budget below.
const branchRowFixed = 4 + 3 + 3 + 2 + 2 + 2 + 2

// branchColumns sizes a branch row's variable columns against the panel the row
// actually has to fit in, rather than against the longest string in the store.
//
// The order of allocation *is* the fix. The status cluster is what a branch row
// exists to show — it is costed first and never squeezed. The target column
// takes what it needs. The branch name absorbs whatever is left and elides in
// place, so a long name shortens itself instead of shoving the signals off the
// right-hand end for clipRow to cut (roadmap area 15).
//
// "Whatever is left" is genuinely what is left. Area 15 capped this column at 32
// cells, which protected a narrow terminal and taxed a wide one: at 110 columns
// a 56-cell branch name was cut with forty cells sitting empty beside it, since
// the clamp ran before the arithmetic below ever reached them (roadmap area 20).
//
// Widths are measured over every ticket, not just the expanded ones, so the
// columns stay aligned down the whole dashboard as tickets expand and collapse
// (DESIGN.md §1).
func (m Model) branchColumns() branchCols {
	name, ab := 0, 0
	for _, t := range m.store.Tickets {
		for _, br := range t.Branches {
			if w := lipgloss.Width(br.Branch); w > name {
				name = w
			}
			if w := lipgloss.Width(m.renderAheadBehind(br, m.status[statusKey(t.ID, br.Branch)])); w > ab {
				ab = w
			}
		}
	}
	target := m.targetKeyWidth

	cw := rowWidth(m.styles, m.width)
	if cw <= 0 {
		return branchCols{name: name, target: target, ab: ab} // size unknown: natural sizing
	}

	// What the name and target columns have to share, once the cluster is paid
	// for. The floor is a guard, not a mechanism — a terminal narrow enough to
	// reach it is below minTerminalWidth and never drawn.
	avail := cw - branchRowFixed - ab
	if avail < minNameCol {
		avail = minNameCol
	}
	if target > avail-minNameCol {
		if target = avail - minNameCol; target < 0 {
			target = 0
		}
	}
	if name > avail-target {
		name = avail - target
	}
	if name < 1 {
		name = 1
	}
	return branchCols{name: name, target: target, ab: ab}
}

// branchRow renders one paired branch beneath its ticket, with the status
// cluster in the fixed order from DESIGN.md §1: target · ↓behind ↑ahead ·
// unpublished · dirty · checked-out marker, then the area-5 unmergeable marker
// at the end so it never disturbs the aligned columns.
func (m Model) branchRow(ticketID string, br store.TicketBranch, cols branchCols) string {
	st := m.status[statusKey(ticketID, br.Branch)]

	name := m.styles.branch.Render("    " + fit(br.Branch, cols.name))
	target := m.styles.target.Render(fit(br.TargetKey, cols.target))
	// The status pair is padded to its column too: without it a ↓3 ↑1 row and a
	// ↓12 ↑345 row put the dirty dot in different places, and the cluster stops
	// being something the eye can scan down.
	ab := fit(m.renderAheadBehind(br, st), cols.ab)

	dirty := "  "
	if br.Branch == m.current && m.dirty {
		dirty = m.styles.dirty.Render("● ")
	}

	marker := "  "
	if br.Branch == m.current {
		marker = m.styles.marker.Render("▸ ")
	}

	row := fmt.Sprintf("%s   %s   %s  %s%s%s", name, target, ab, m.renderPublish(st), dirty, marker)

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

// renderPublish draws the branch's standing with its *own* remote (roadmap
// 17b): `⇡` when it holds commits origin/<branch> does not, `⊘` when it has no
// upstream at all, blank when it is published and current.
//
// A glyph rather than a count, deliberately. `↑ahead` is already on this row and
// is measured against the target, so a second number would put two up-arrows
// with two different denominators side by side and ask the reader to remember
// which was which. A glyph reads as a *state* — there is work here that has not
// left this machine — which is the whole of what the signal has to say, and it
// is what makes `s` and `u` legible on screen: `s` leaves `⇡`, `u` clears it.
//
// It takes the dirty style for the reason area 6's held-tracked glyph does:
// that colour has always meant "work that exists only here", and uncommitted and
// unpublished are the two kinds of it. `behind` stays the only alarm on screen,
// so this adds no new one. `⊘` recedes into the hint style instead — an
// unpublished branch is a fact about the branch, not something that went wrong.
//
// Two cells wide whichever state it is in, so the two glyphs beside it stay in
// the same column down the whole list (DESIGN.md §1).
func (m Model) renderPublish(st branchStatus) string {
	switch {
	case st.noUpstream:
		return m.styles.help.Render("⊘ ")
	case st.unpublished > 0:
		return m.styles.dirty.Render("⇡ ")
	}
	return "  "
}

// statusLine surfaces the last error or transient notice under the panel.
//
// Both are routed through chromeText, and the reason is a correctness one
// rather than a cosmetic one: a git error is unbounded *and* multi-line, and
// the frame budgets this line as exactly one (listChrome). See chrome.go.
func (m Model) statusLine() string {
	if m.err != nil {
		return m.styles.errText.Render(chromeText(m.styles, m.width, "error: "+m.err.Error()))
	}
	if m.notice != "" {
		return m.styles.hint.Render(chromeText(m.styles, m.width, m.notice))
	}
	return ""
}

// help is the dashboard's key line, elided against the real width.
//
// The lead is ordered by what a reader needs first, because that order is now
// load-bearing: a narrow terminal keeps the front of the line and marks the
// rest. Move and open come first, then the two verbs that build the list at
// all — a new user on an 80-column terminal must still be told how to add a
// ticket — then the sweep and the three screens. `? help` anchors the line
// because it is where everything elided still lives.
//
// `t targets` is last in the lead, and so the first thing a narrow terminal
// drops. It is the one segment here that opens something you *read* rather than
// something you do, so it is what the line can most afford to lose — and the
// overlay it is elided into names it in full.
//
// `p re-pair` sits behind the sweep rather than beside `u` and `s`, and that is
// the same ordering rule rather than an exception to it. This line is sorted by
// *frequency*, not by kind: `p` is a doing verb, but it is a correction made once
// for a pairing that was wrong, where `r` and `f` are pressed all day. Measured,
// because it is the kind of claim that is cheap to get wrong — placing it here
// leaves the line at 120 columns exactly as it shipped, and costs `t targets`
// its slot at 140.
func (m Model) help() string {
	if m.screen == screenConfirmDelete {
		return helpLine(m.styles, m.width, nil, []string{"y confirm", "n cancel"})
	}
	return helpLine(m.styles, m.width,
		[]string{"j/k move", "enter expand/diff", "a add", "d delete", "u update", "s shelve", "r refresh", "f fetch", "p re-pair", "l local", "t targets"},
		[]string{"? help", "q quit"})
}

// addIDView is the ticket-ID entry screen.
func (m Model) addIDView() string {
	panel := m.styles.hint.Render("New ticket") + "\n\n" +
		"  ID  " + m.input.View()
	help := helpLine(m.styles, m.width, nil, []string{"enter continue", "esc cancel"})
	return m.screenView(panel, help)
}

// pairingView is the candidate checklist, or the target picker overlay drawn in
// its place while it is open.
func (m Model) pairingView() string {
	if m.add.picker {
		help := helpLine(m.styles, m.width, []string{"j/k move"}, []string{"enter select", "esc back"})
		return m.screenView(m.pickerBody(), help)
	}
	if m.add.filter.open {
		help := helpLine(m.styles, m.width,
			[]string{"type to filter", "↑/↓ move"}, []string{"enter accept", "esc clear"})
		return m.screenView(m.pairingBody(), help)
	}
	// esc means one thing at a time, and the line says which is live — so the
	// segment naming it is an anchor, never something a narrow terminal elides.
	if m.add.filter.active() {
		help := helpLine(m.styles, m.width,
			[]string{"space toggle", "t target", "1–9 quick-target", "/ refine"},
			[]string{"enter save", "esc clear filter"})
		return m.screenView(m.pairingBody(), help)
	}
	help := helpLine(m.styles, m.width,
		[]string{"space toggle", "t target", "1–9 quick-target", "/ filter"},
		[]string{"enter save", "esc cancel"})
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

	vis := m.add.visible()
	if m.add.filter.open || m.add.filter.active() {
		hidden := hiddenSelectedCount(len(m.add.candidates), vis,
			func(i int) bool { return m.add.candidates[i].included })
		header = append(header,
			m.add.filter.line(m.styles, len(vis), len(m.add.candidates), hidden), "")
	}
	if len(vis) == 0 {
		return strings.Join(append(header,
			m.styles.help.Render("No candidate matches "+quote(m.add.filter.query())+" — esc clears the filter.")), "\n")
	}

	// The assignment cell is what the checklist is *for* — it is built first and
	// costed first, and the name column takes what is left, the same ordering the
	// dashboard's branch rows use.
	assigns := make([]string, len(vis))
	for i, ci := range vis {
		c := m.add.candidates[ci]
		switch {
		case !c.included:
		case c.targetKey == "":
			assigns[i] = m.styles.errText.Render("⚠ pick a target")
		default:
			assigns[i] = m.styles.target.Render("→ " + c.targetKey)
		}
	}

	nameWidth := m.candidateNameWidth(vis, widestCell(len(assigns), func(i int) string { return assigns[i] }))
	rows := make([]string, len(vis))
	for i, ci := range vis {
		box := "[ ]"
		if m.add.candidates[ci].included {
			box = "[x]"
		}
		rows[i] = fmt.Sprintf("%s %s  %s", box, fit(m.add.candidates[ci].branch, nameWidth), assigns[i])
	}
	// The cursor indexes the *visible* rows, so the detail comes from vis rather
	// than from candidates — the same indirection every other read on this screen
	// makes once a filter is applied (filter.go).
	detail := ""
	if c := m.add.cursor; c >= 0 && c < len(vis) {
		detail = m.add.candidates[vis[c]].branch
	}
	return listBody(m.styles, m.width, m.height, header, rows, m.add.cursor, detail)
}

// pickerBody is the target picker over the pairing checklist, about the selected
// candidate. A candidate assigned earlier in the same visit has its target marked
// like any other current value — it is what re-opening the picker is for.
func (m Model) pickerBody() string {
	cand, current := "", ""
	if ci, ok := m.add.selected(); ok {
		cand = m.add.candidates[ci].branch
		current = m.add.candidates[ci].targetKey
	}
	return m.targetPickerBody(cand, current, m.add.pickerCur)
}

// targetPickerRowFixed is what a picker row costs before its two variable
// columns: the accelerator cell, the space after it, and the two separators
// before the ref and before the current-target mark.
const targetPickerRowFixed = 2 + 1 + 2 + 2

// targetPickerBody lists every configured target for one branch, showing Key and
// Ref so a terse key is never ambiguous. The 1–9 accelerators are shown against
// the first nine; the rest are reachable by moving the cursor.
//
// One body for both places the overlay opens — over the pairing checklist and,
// since 19b, over a dashboard branch row — because it is one overlay asking one
// question. Two renderings of the same choice would be two things to keep in step,
// and DESIGN.md §2's rule is that an overlay is an overlay wherever the user
// meets one.
//
// The subject and the current target are named rather than read from either
// screen's state: they are the whole of what differs between the two, and passing
// them in is what lets the rest be shared.
//
// **The current target is marked**, which is 19e's argument about its ref picker
// applied to this one: the cursor opens on the current value, but that signal is
// gone the moment the user moves, and a list with nothing distinguishing one row
// from another cannot say what is being changed *from*. It is a word rather than
// a glyph because this overlay binds no `?` — a glyph here would be one with
// nowhere to be explained (DESIGN.md §3).
func (m Model) targetPickerBody(subject, current string, cursor int) string {
	header := []string{m.styles.hint.Render("Target for " + subject), ""}

	labels := make([]string, len(m.cfg.Targets))
	for i, t := range m.cfg.Targets {
		if current != "" && t.Key == current {
			labels[i] = m.styles.help.Render(currentRefLabel)
		}
	}
	labelWidth := widestCell(len(labels), func(i int) string { return labels[i] })

	// The label is a fixed cost paid before the ref, and the ref absorbs what is
	// left — the allocation rule every row here follows (DESIGN.md §1). Sizing it
	// is what keeps the mark on screen: left unsized, a long ref would push the one
	// cell saying "this is the one you have now" off the end for clipRow to cut.
	//
	// The key is squeezed against the ref's floor before the ref gives way at all,
	// which is what the departed maxTargetCol used to do by accident: on this
	// screen the key is what you are choosing, so it is costed first, but a key
	// nothing enforces the terseness of must not be able to take the whole row
	// (roadmap area 20).
	keyWidth := m.targetKeyWidth
	refWidth := widestCell(len(m.cfg.Targets), func(i int) string { return m.cfg.Targets[i].Ref })
	if cw := rowWidth(m.styles, m.width); cw > 0 {
		avail := cw - targetPickerRowFixed - labelWidth
		if keyWidth > avail-minRefCol {
			if keyWidth = avail - minRefCol; keyWidth < 0 {
				keyWidth = 0
			}
		}
		if refWidth > avail-keyWidth {
			if refWidth = avail - keyWidth; refWidth < minRefCol {
				refWidth = minRefCol
			}
		}
	}

	rows := make([]string, len(m.cfg.Targets))
	for i, t := range m.cfg.Targets {
		acc := "  "
		if i < 9 {
			acc = fmt.Sprintf("%d ", i+1)
		}
		rows[i] = fmt.Sprintf("%s %s  %s  %s",
			m.styles.help.Render(acc), fit(t.Key, keyWidth),
			m.styles.help.Render(fit(t.Ref, refWidth)), labels[i])
	}
	// The ref, not the key: the key is costed first here because it is what you
	// are choosing, so the ref is the cell that gives way, and it is the unbounded
	// one either way. One row carries two values that can elide, and the detail
	// line takes the one the allocation above spends last.
	detail := ""
	if cursor >= 0 && cursor < len(m.cfg.Targets) {
		detail = m.cfg.Targets[cursor].Ref
	}
	return listBody(m.styles, m.width, m.height, header, rows, cursor, detail)
}

// candidateNameWidth sizes the checklist's name column: the widest candidate,
// bounded only by what the panel has left once the box, the separators and the
// assignment cell are paid for. A long name elides in place rather than pushing
// "⚠ pick a target" — the one thing on the row that blocks the save — off the
// end.
//
// Measured over the visible rows only: the column aligns what is on screen, so a
// name the filter is hiding has no business padding the rows that are drawn
// (DESIGN.md §1).
func (m Model) candidateNameWidth(visible []int, assignWidth int) int {
	// box + the two separators around the name.
	const fixed = 3 + 1 + 2

	w := widestCell(len(visible), func(i int) string {
		return m.add.candidates[visible[i]].branch
	})
	cw := rowWidth(m.styles, m.width)
	if cw <= 0 {
		return w // size unknown: natural sizing
	}
	if avail := cw - fixed - assignWidth; w > avail {
		if w = avail; w < minNameCol {
			w = minNameCol
		}
	}
	return w
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
