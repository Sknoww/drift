package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Sknoww/drift/internal/store"
)

// --- the window itself ------------------------------------------------------

func TestRowWindowDrawsEverythingThatFits(t *testing.T) {
	start, end := rowWindow(10, 3, 20)
	if start != 0 || end != 10 {
		t.Errorf("rowWindow(10, 3, 20) = %d..%d, want 0..10 — a list that fits is never clipped", start, end)
	}
}

// Before the first WindowSizeMsg the terminal size is genuinely unknown.
// Clipping to a guess there would hide rows on a terminal with room for them.
func TestRowWindowIsUnclippedBeforeTheFirstSize(t *testing.T) {
	start, end := rowWindow(5000, 4999, 0)
	if start != 0 || end != 5000 {
		t.Errorf("unknown size: rowWindow = %d..%d, want the whole list", start, end)
	}
}

// The acceptance test for the whole area, at the window level: the cursor is
// inside the drawn range at *every* position, and the drawn range plus its edge
// markers never exceeds the budget. That second half is what actually bounds the
// frame — a window that kept the cursor visible by growing would not.
func TestRowWindowKeepsTheCursorVisibleWithinBudget(t *testing.T) {
	for _, capacity := range []int{3, 5, 20, 47} {
		for _, n := range []int{48, 400, 5000} {
			for selected := 0; selected < n; selected++ {
				start, end := rowWindow(n, selected, capacity)

				if selected < start || selected >= end {
					t.Fatalf("capacity %d, n %d, cursor %d: window %d..%d leaves the cursor off screen",
						capacity, n, selected, start, end)
				}

				drawn := end - start
				if start > 0 {
					drawn++ // "↑ N more"
				}
				if end < n {
					drawn++ // "↓ N more"
				}
				if drawn > capacity {
					t.Fatalf("capacity %d, n %d, cursor %d: %d lines drawn — the frame is unbounded",
						capacity, n, selected, drawn)
				}
			}
		}
	}
}

// A flush edge carries no marker, so the line reserved for it is spent on a row
// instead of left empty. Without this the top and bottom of every long list show
// one row fewer than the terminal can hold.
func TestRowWindowSpendsTheFlushEdgesLine(t *testing.T) {
	const capacity = 10

	start, end := rowWindow(100, 0, capacity)
	if start != 0 || end != capacity-1 {
		t.Errorf("at the top: window = %d..%d, want 0..%d (rows + one ↓ marker)", start, end, capacity-1)
	}

	start, end = rowWindow(100, 99, capacity)
	if end != 100 || start != 100-(capacity-1) {
		t.Errorf("at the bottom: window = %d..%d, want %d..100 (one ↑ marker + rows)",
			start, end, 100-(capacity-1))
	}
}

// --- listBody ---------------------------------------------------------------

func TestListBodyMarksBothClippedEdges(t *testing.T) {
	rows := make([]string, 100)
	for i := range rows {
		rows[i] = fmt.Sprintf("row-%03d", i)
	}
	// height 25 - 5 chrome - 1 header - 1 detail = 18 rows of budget.
	body := listBody(newStyles(store.Prefs{}), 80, 25, []string{"header"}, rows, 50, "row-050")

	if !strings.Contains(body, "↑ ") || !strings.Contains(body, "↓ ") {
		t.Errorf("a list clipped at both ends must say so; got:\n%s", body)
	}
	if !strings.Contains(body, "row-050") {
		t.Errorf("the cursor's row is not on screen; got:\n%s", body)
	}
	if strings.Contains(body, "row-000") || strings.Contains(body, "row-099") {
		t.Errorf("rows outside the window were drawn; got:\n%s", body)
	}
	if got, want := strings.Count(body, "\n")+1, 20; got != want {
		t.Errorf("body = %d lines, want %d (1 header + 18 budget + 1 detail)", got, want)
	}
}

// A list with no active selection still windows — from the top, and without a
// band. Passing -1 must not be read as "row zero".
func TestListBodyWithoutASelection(t *testing.T) {
	rows := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	body := listBody(newStyles(store.Prefs{}), 80, 12, nil, rows, -1, "")

	if !strings.Contains(body, "a") || strings.Contains(body, "h") {
		t.Errorf("an unselected list should window from the top; got:\n%s", body)
	}
}

// --- the detail line (roadmap area 20b) -------------------------------------

// The floor the area exists for: a value no column could show is readable under
// the list, whole. The row above it is cut; the line below it is not.
func TestTheDetailLineCarriesAValueTheRowCouldNotShow(t *testing.T) {
	s := newStyles(store.Prefs{})
	const long = "main-connector/src/main/java/com/teamviewer/connector/logging/Log4j2Configurer.java"

	rows := []string{fit(long, 40), fit("short.txt", 40)}
	body := listBody(s, 110, 24, nil, rows, 0, long)

	last := lastLine(body)
	if !strings.Contains(last, long) {
		t.Errorf("the detail line does not carry the value it exists for:\n%q", last)
	}
	if lipgloss.Width(rows[0]) >= lipgloss.Width(long) {
		t.Fatal("the fixture row is not actually cut — the test asserts nothing")
	}
}

// The redundancy rule: a row that already shows the value in full has nothing to
// explain, and repeating it there would read as a second value rather than as
// the same one.
func TestTheDetailLineIsBlankWhenTheRowShowsTheValueWhole(t *testing.T) {
	s := newStyles(store.Prefs{})

	rows := []string{fit("feature/short", 40), fit("other", 40)}
	if got := lastLine(listBody(s, 110, 24, nil, rows, 0, "feature/short")); strings.TrimSpace(got) != "" {
		t.Errorf("a whole value was repeated under its own row: %q", got)
	}
}

// **The reserve-always rule, which is the whole reason the line is unconditional.**
// Drawing it only when there is something to say makes the panel grow and shrink
// as the cursor moves between a cut row and a whole one — the defect listChrome's
// own comment calls out for the status line, one line lower down.
func TestTheDetailLineIsReservedWhetherOrNotItIsDrawn(t *testing.T) {
	s := newStyles(store.Prefs{})
	const long = "update/PSOT-22223/audit-workflow-migration-to-vscode-mvp3"

	rows := []string{fit(long, 20), fit("brief", 20)}
	cut := listBody(s, 110, 24, nil, rows, 0, long)
	whole := listBody(s, 110, 24, nil, rows, 1, "brief")

	if a, b := strings.Count(cut, "\n"), strings.Count(whole, "\n"); a != b {
		t.Errorf("the panel changed height with the cursor: %d lines cut, %d whole", a+1, b+1)
	}
	if got := lastLine(whole); strings.TrimSpace(got) != "" {
		t.Errorf("the reserved line should be blank here, got %q", got)
	}
}

// A value too long even for the whole panel is elided by the package's one rule
// rather than wrapping. One line was the budget; wrapping would spend a second
// the frame never counted, which is how a header wrapping ran the wizard off the
// top of the terminal in area 15.
func TestTheDetailLineElidesRatherThanWrapping(t *testing.T) {
	s := newStyles(store.Prefs{})
	const width = 80
	long := strings.Repeat("a/very-long-segment", 20)

	body := listBody(s, width, 24, nil, []string{fit(long, 20)}, 0, long)
	// Measured against the panel, not against a row: the line carries the
	// selection gutter in front of it like the edge markers do, so its budget is
	// the gutter plus what a row has — which is the panel exactly.
	last := lastLine(body)
	if w, cw := lipgloss.Width(last), contentWidth(s, width); w > cw {
		t.Errorf("the detail line is %d wide, the panel is %d — it would wrap", w, cw)
	}
	if !strings.Contains(last, "…") {
		t.Errorf("an over-long detail should say what it cut: %q", last)
	}
}

// A list with no selection has no row the detail could be about, and a screen
// whose values are literals has no value to report. Both still reserve the line.
func TestTheDetailLineIsBlankWithoutASelectionOrAValue(t *testing.T) {
	s := newStyles(store.Prefs{})
	rows := []string{"alpha", "bravo"}

	for _, tc := range []struct {
		name     string
		selected int
		detail   string
	}{
		{"no selection", -1, "alpha"},
		{"no value", 0, ""},
	} {
		body := listBody(s, 110, 24, nil, rows, tc.selected, tc.detail)
		if got := lastLine(body); strings.TrimSpace(got) != "" {
			t.Errorf("%s: want a blank reserved line, got %q", tc.name, got)
		}
		if got, want := strings.Count(body, "\n")+1, len(rows)+detailLines; got != want {
			t.Errorf("%s: body = %d lines, want %d", tc.name, got, want)
		}
	}
}

// The capacity arithmetic: the detail line comes out of the same budget the rows
// do, so a list that overflows draws one row fewer than it did before 20b rather
// than one line more than the terminal holds.
func TestTheDetailLineIsPaidForOutOfTheRowBudget(t *testing.T) {
	s := newStyles(store.Prefs{})
	const height = 25

	rows := make([]string, 100)
	for i := range rows {
		rows[i] = fmt.Sprintf("row-%03d", i)
	}
	header := []string{"header"}

	body := listBody(s, 80, height, header, rows, 50, "row-050")
	got := strings.Count(body, "\n") + 1

	want := len(header) + listCapacity(height, len(header)+detailLines) + detailLines
	if got != want {
		t.Errorf("body = %d lines, want %d", got, want)
	}
	if got > height-listChrome+len(header) {
		t.Errorf("body = %d lines, more than the frame budgets at height %d", got, height)
	}
}

// lastLine is the detail line: listBody always ends with it, drawn or blank.
func lastLine(body string) string {
	lines := strings.Split(body, "\n")
	return lines[len(lines)-1]
}

// --- the wizard: the screen this area exists for ----------------------------

// The bug a user actually hit: the first-run wizard on a repo with hundreds of
// remote refs rendered every one of them, ran off the top of the terminal, and
// froze hard enough to force-quit the window. The frame must stay inside the
// terminal at every cursor position, with the selected ref on screen.
func TestWizardBoundsTheFrameAtEveryCursorPosition(t *testing.T) {
	const (
		refs   = 400
		width  = 100
		height = 24
	)
	remotes := make([]string, refs)
	for i := range remotes {
		remotes[i] = fmt.Sprintf("origin/b%04d", i) // fixed width: no wrapping, no prefix collisions
	}

	var m tea.Model = newWizard(remoteBranches(remotes...), store.Prefs{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: height})

	for cursor := 0; cursor < refs; cursor++ {
		w := m.(wizardModel)
		if w.cursor != cursor {
			t.Fatalf("cursor = %d, want %d", w.cursor, cursor)
		}

		view := w.View()
		if lines := strings.Count(view, "\n") + 1; lines > height {
			t.Fatalf("cursor %d: frame is %d lines on a %d-line terminal — it runs off the top",
				cursor, lines, height)
		}
		if ref := remotes[cursor]; !strings.Contains(view, ref) {
			t.Fatalf("cursor %d: selected ref %q is not on screen", cursor, ref)
		}
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}
}

// Windowing bounds the row count; clipping bounds what each row costs. Without
// the second the first is fiction — long branch names are exactly what real
// repos have, and a window of rows that each wrap is twice the frame it was
// budgeted for. Same assertion as above, with names long enough to overflow.
func TestWizardBoundsTheFrameWithLongRefNames(t *testing.T) {
	const (
		refs   = 400
		width  = 100
		height = 24
	)
	remotes := make([]string, refs)
	for i := range remotes {
		remotes[i] = fmt.Sprintf("origin/feature/TEAM-%04d-a-rather-long-branch-name-of-the-kind-real-repos-have", i)
	}

	var m tea.Model = newWizard(remoteBranches(remotes...), store.Prefs{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: height})

	for cursor := 0; cursor < refs; cursor++ {
		view := m.(wizardModel).View()
		if lines := strings.Count(view, "\n") + 1; lines > height {
			t.Fatalf("cursor %d: frame is %d lines on a %d-line terminal — rows are wrapping",
				cursor, lines, height)
		}
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}
}

// A clipped row keeps its own width exactly — one cell over and Lip Gloss wraps
// it, which is the whole failure being prevented.
//
// The width a row has is rowWidth, not contentWidth: under a treatment that
// draws a left-edge marker the gutter is not the row's to spend, and clipping to
// the panel would let every row overflow by exactly the gutter (band.go).
func TestClipRowCapsAtTheRowWidth(t *testing.T) {
	s := newStyles(store.Prefs{})
	for _, width := range []int{40, 80, 100} {
		rw := rowWidth(s, width)
		long := s.branch.Render(strings.Repeat("x", rw+50))

		if got := lipgloss.Width(clipRow(s, width, long)); got != rw {
			t.Errorf("width %d: clipped row = %d cells, want %d", width, got, rw)
		}
		// A panel line drawn without a cursor — the help overlay — has no gutter
		// to reserve and gets the whole content width.
		cw := contentWidth(s, width)
		if got := lipgloss.Width(clipPanelLine(s, width, long)); got != cw {
			t.Errorf("width %d: clipped panel line = %d cells, want %d", width, got, cw)
		}
		// A row that already fits is returned untouched, ellipsis and all absent.
		short := s.branch.Render("fits")
		if got := clipRow(s, width, short); got != short {
			t.Errorf("width %d: a fitting row was altered: %q", width, got)
		}
	}
}

// Windowing is a render concern only: a ref scrolled out of view is still
// selected, and still saved. The same "never guess" rule as pairing — the fix
// for a long list must not quietly drop what the user picked.
func TestWizardSavesSelectionsScrolledOutOfView(t *testing.T) {
	remotes := make([]string, 200)
	for i := range remotes {
		remotes[i] = fmt.Sprintf("origin/b%04d", i)
	}

	var m tea.Model = newWizard(remoteBranches(remotes...), store.Prefs{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace}) // select the first ref

	for i := 0; i < 150; i++ { // scroll it far out of the window
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}
	if strings.Contains(m.(wizardModel).View(), "origin/b0000") {
		t.Fatal("the test is not exercising what it claims: the ref is still on screen")
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	w := m.(wizardModel)
	if !w.done || len(w.result) != 1 || w.result[0].Ref != "origin/b0000" {
		t.Errorf("save = %+v (done %v), want the off-screen selection kept", w.result, w.done)
	}
}

// --- the dashboard and its overlays -----------------------------------------

func TestDashboardBoundsTheFrameAtEveryCursorPosition(t *testing.T) {
	const height = 24

	tickets := make([]store.Ticket, 120)
	for i := range tickets {
		tickets[i] = store.Ticket{
			ID:       fmt.Sprintf("ABC-%04d", i),
			Branches: []store.TicketBranch{{Branch: fmt.Sprintf("b%04d", i), TargetKey: "main"}},
		}
	}

	m := New(nil, samplePaths(), sampleConfig(), store.Store{Tickets: tickets}, store.Prefs{})
	m.loading = false
	m.width, m.height = 100, height
	for _, t := range tickets {
		m.expanded[t.ID] = true // branch rows too: 240 rows over a 24-line terminal
	}

	for cursor := 0; cursor < len(m.visibleRows()); cursor++ {
		m.cursor = cursor
		view := m.View()
		if lines := strings.Count(view, "\n") + 1; lines > height {
			t.Fatalf("cursor %d: frame is %d lines on a %d-line terminal", cursor, lines, height)
		}

		row, _ := m.selectedRow()
		want := tickets[row.ticket].ID
		if row.isBranch() {
			want = tickets[row.ticket].Branches[row.branch].Branch
		}
		if !strings.Contains(view, want) {
			t.Fatalf("cursor %d: selected row %q is not on screen", cursor, want)
		}
	}
}

func TestPairingChecklistWindows(t *testing.T) {
	const height = 24

	m := newModel()
	m.width, m.height = 100, height
	m.screen = screenPairing
	m.add = addFlow{id: "ABC-1", loaded: true}
	for i := 0; i < 300; i++ {
		m.add.candidates = append(m.add.candidates, candidate{branch: fmt.Sprintf("abc-1-v%04d", i)})
	}
	m.add.cursor = 299

	view := m.View()
	if lines := strings.Count(view, "\n") + 1; lines > height {
		t.Errorf("frame is %d lines on a %d-line terminal", lines, height)
	}
	if !strings.Contains(view, "abc-1-v0299") {
		t.Errorf("the selected candidate is not on screen; got:\n%s", view)
	}
}

func TestLocalOnlyListWindows(t *testing.T) {
	const height = 24

	m := newModel()
	m.width, m.height = 100, height
	m.screen = screenLocalOnly
	m.local = localOnlyState{loaded: true}
	for i := 0; i < 300; i++ {
		m.local.entries = append(m.local.entries, heldPath{path: fmt.Sprintf("p%04d.yml", i), tracked: true})
	}
	m.local.cursor = 299

	view := m.View()
	if lines := strings.Count(view, "\n") + 1; lines > height {
		t.Errorf("frame is %d lines on a %d-line terminal", lines, height)
	}
	if !strings.Contains(view, "p0299.yml") {
		t.Errorf("the selected held path is not on screen; got:\n%s", view)
	}
}
