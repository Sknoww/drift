package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Sknoww/drift/internal/store"
)

// --- fit --------------------------------------------------------------------

// The defect fit replaced: padRight returned long input *unchanged*, so every
// "capped" column was only capped against over-padding. A cell must come back at
// exactly the width its column was given, whichever side of it the input starts.
func TestFitRendersExactlyItsColumnWidth(t *testing.T) {
	cases := []struct {
		in   string
		w    int
		want string
	}{
		{"main", 8, "main    "},    // short: padded
		{"main", 4, "main"},        // exact: untouched
		{"feature/x", 6, "featu…"}, // long: truncated, and the ellipsis is part of the budget
		{"main", 0, ""},
	}
	for _, c := range cases {
		if got := fit(c.in, c.w); got != c.want {
			t.Errorf("fit(%q, %d) = %q, want %q", c.in, c.w, got, c.want)
		}
		if c.w > 0 {
			if got := lipgloss.Width(fit(c.in, c.w)); got != c.w {
				t.Errorf("fit(%q, %d) is %d cells wide, want %d", c.in, c.w, got, c.w)
			}
		}
	}
}

// The two padding implementations disagreed on how to measure, and the one used
// most measured with len(): any non-ASCII or wide rune in a branch name
// misaligned every column beside it on the dashboard.
func TestFitMeasuresDisplayWidthNotBytes(t *testing.T) {
	// "café" is 5 bytes and 4 cells; "日本" is 6 bytes and 4 cells.
	for _, s := range []string{"café", "日本"} {
		if got := lipgloss.Width(fit(s, 10)); got != 10 {
			t.Errorf("fit(%q, 10) is %d cells wide, want 10 — measured by bytes, not display width", s, got)
		}
	}
	if got := fit("café", 4); got != "café" {
		t.Errorf("fit(%q, 4) = %q — a 4-cell string in a 4-cell column must be untouched", "café", got)
	}
}

// Some cells arrive already styled (the help overlay's glyph legend renders each
// glyph in its own role's color before it reaches the column). Cutting one by
// bytes would sever an escape sequence and bleed color down the frame — the same
// hazard clipRow exists to avoid.
func TestFitIsANSIAware(t *testing.T) {
	s := lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render("abcdefghij")
	got := fit(s, 5)

	if w := lipgloss.Width(got); w != 5 {
		t.Errorf("styled cell fitted to 5 is %d cells wide", w)
	}
	if strings.Count(got, "\x1b[") > 0 && !strings.Contains(got, ansiReset) {
		t.Errorf("truncation dropped the closing reset, leaking style into the rest of the row: %q", got)
	}
}

func TestPadLeftMeasuresDisplayWidth(t *testing.T) {
	if got := lipgloss.Width(padLeft("2d", 4)); got != 4 {
		t.Errorf("padLeft(\"2d\", 4) is %d cells wide, want 4", got)
	}
	if got := padLeft("12mo", 4); got != "12mo" {
		t.Errorf("padLeft at exactly the column width altered the cell: %q", got)
	}
}

func TestWidestCellHonoursItsCap(t *testing.T) {
	cells := []string{"a", "bbbbbbbbbb", "cc"}
	at := func(i int) string { return cells[i] }

	if got := widestCell(len(cells), 0, at); got != 10 {
		t.Errorf("unbounded widestCell = %d, want 10", got)
	}
	if got := widestCell(len(cells), 4, at); got != 4 {
		t.Errorf("capped widestCell = %d, want 4", got)
	}
	if got := widestCell(len(cells), 40, at); got != 10 {
		t.Errorf("a cap above the content = %d, want 10 — a cap is a ceiling, not a width", got)
	}
}

// --- the dashboard's column budget ------------------------------------------

// The headline defect of the area: branchNameWidth capped at 32 and padRight
// ignored the cap, so a 60-character branch name rendered all 60 and shoved the
// status cluster off the right-hand end — where clipRow then silently cut it.
// The name must shorten itself and the signals must survive.
func TestLongBranchNameKeepsTheStatusCluster(t *testing.T) {
	const width = 100

	m := newModel()
	m.width, m.height = width, 24
	m.store = store.Store{Tickets: []store.Ticket{{
		ID: "ABC-1",
		Branches: []store.TicketBranch{
			{Branch: strings.Repeat("long-branch-name-", 4), TargetKey: "main"},
		},
	}}}
	m.expanded["ABC-1"] = true
	m.status[statusKey("ABC-1", m.store.Tickets[0].Branches[0].Branch)] = branchStatus{
		known: true, behind: 3, ahead: 1,
	}

	row := m.branchRow("ABC-1", m.store.Tickets[0].Branches[0], m.branchColumns())

	if !strings.Contains(row, "↓3") || !strings.Contains(row, "↑1") {
		t.Errorf("the status cluster was pushed off the row by the branch name:\n%s", row)
	}
	if !strings.Contains(row, "main") {
		t.Errorf("the target column was pushed off the row:\n%s", row)
	}
	if !strings.Contains(row, "…") {
		t.Errorf("the long name was not ellipsised in place:\n%s", row)
	}
	if w := lipgloss.Width(row); w > contentWidth(m.styles, width) {
		t.Errorf("row is %d cells wide, want at most %d — clipRow would have to cut it",
			w, contentWidth(m.styles, width))
	}
}

// A column is a ceiling, not a fixed width: short names must not be padded out
// to the cap, or every dashboard is mostly whitespace.
func TestBranchColumnsShrinkToTheirContent(t *testing.T) {
	m := newModel()
	m.width, m.height = 100, 24

	cols := m.branchColumns()
	if cols.name != len("abc-1-perf") {
		t.Errorf("name column = %d, want %d — it grew past its content", cols.name, len("abc-1-perf"))
	}
	if cols.target != len("r2perf") {
		t.Errorf("target column = %d, want %d", cols.target, len("r2perf"))
	}
}

// The order of allocation is the fix: the cluster is costed first, so a terminal
// too narrow for everything squeezes the name rather than the signals.
func TestNarrowTerminalSqueezesTheNameNotTheCluster(t *testing.T) {
	m := newModel()
	m.height = 24
	m.expanded["ABC-1"] = true
	br := m.store.Tickets[0].Branches[0]
	m.status[statusKey("ABC-1", br.Branch)] = branchStatus{known: true, behind: 12, ahead: 345}

	wide := func() int { m.width = 200; return m.branchColumns().name }()
	narrow := func() int { m.width = minTerminalWidth; return m.branchColumns().name }()

	if narrow >= wide {
		t.Errorf("name column: %d at 200 cols, %d at %d — it did not give way", wide, narrow, minTerminalWidth)
	}
	if narrow < minNameCol {
		t.Errorf("name column squeezed to %d, below the %d floor", narrow, minNameCol)
	}

	row := m.branchRow("ABC-1", br, m.branchColumns())
	if !strings.Contains(row, "↓12") || !strings.Contains(row, "↑345") {
		t.Errorf("the cluster gave way instead of the name:\n%s", row)
	}
}

// DESIGN.md §1: the cluster is "aligned into columns so the eye scans down".
// Without padding the status pair, a ↓3 ↑1 row and a ↓12 ↑345 row put the dirty
// dot and the checked-out marker in different places.
func TestStatusPairAlignsAcrossRows(t *testing.T) {
	m := newModel()
	m.width, m.height = 100, 24
	m.expanded["ABC-1"] = true

	brs := m.store.Tickets[0].Branches
	m.status[statusKey("ABC-1", brs[0].Branch)] = branchStatus{known: true, behind: 3, ahead: 1}
	m.status[statusKey("ABC-1", brs[1].Branch)] = branchStatus{known: true, behind: 12, ahead: 345}

	cols := m.branchColumns()
	a := lipgloss.Width(m.branchRow("ABC-1", brs[0], cols))
	b := lipgloss.Width(m.branchRow("ABC-1", brs[1], cols))
	if a != b {
		t.Errorf("rows are %d and %d cells wide — the status pair is not a column", a, b)
	}
}

// --- the minimum usable width -----------------------------------------------

// contentWidth used to clamp to 1 rather than declare a floor, so a very narrow
// terminal rendered garbage instead of saying it was too narrow.
func TestTooNarrowTerminalSaysSoRatherThanRenderingGarbage(t *testing.T) {
	m := newModel()
	m.height = 24
	m.expanded["ABC-1"] = true

	for _, width := range []int{20, 40, minTerminalWidth - 1} {
		m.width = width
		view := m.View()

		if !strings.Contains(view, "too narrow") {
			t.Errorf("width %d: rendered a dashboard instead of refusing:\n%s", width, view)
		}
		for _, line := range strings.Split(view, "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("width %d: the too-narrow notice is itself %d cells wide", width, w)
			}
		}
	}

	m.width = minTerminalWidth
	if view := m.View(); strings.Contains(view, "too narrow") {
		t.Errorf("width %d is the floor and must draw:\n%s", minTerminalWidth, view)
	}
}

// The size is genuinely unknown before the first WindowSizeMsg. Refusing to draw
// on a guess would blank the screen on a terminal with room to spare.
func TestUnknownWidthStillDraws(t *testing.T) {
	m := newModel() // width 0: no WindowSizeMsg yet
	if view := m.View(); strings.Contains(view, "too narrow") {
		t.Errorf("refused to draw before the size was known:\n%s", view)
	}
}

// The wizard is its own Bubble Tea program with its own View, so it needs the
// same guard — a squeezed pane must say the same thing wherever the user is.
func TestWizardRefusesToDrawIntoANarrowTerminal(t *testing.T) {
	var m tea.Model = newWizard(remoteBranches("origin/main", "origin/develop"))
	m, _ = m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})

	if view := m.(wizardModel).View(); !strings.Contains(view, "too narrow") {
		t.Errorf("the wizard rendered into a 40-column terminal:\n%s", view)
	}
}

// Declaring minTerminalWidth drawable is a promise that area 14's invariant
// still holds there — and at first it did not. listBody costed its header as
// len(header), but the wizard's two-line intro wraps to three at 60 columns, so
// the window drew one row too many and the frame ran off a 24-line terminal by
// exactly one line. Prose can break the budget just as rows can.
func TestFrameStaysInsideTheTerminalAtTheWidthFloor(t *testing.T) {
	const height = 24

	refs := make([]string, 400)
	for i := range refs {
		refs[i] = fmt.Sprintf("origin/feature/TEAM-%04d-a-rather-long-branch-name", i)
	}

	for _, width := range []int{minTerminalWidth, 70, 80, 100} {
		var m tea.Model = newWizard(remoteBranches(refs...))
		m, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: height})

		for cursor := 0; cursor < len(refs); cursor++ {
			if lines := strings.Count(m.(wizardModel).View(), "\n") + 1; lines > height {
				t.Fatalf("width %d, cursor %d: frame is %d lines on a %d-line terminal",
					width, cursor, lines, height)
			}
			m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		}
	}
}

// The unit underneath it: a header line wider than the panel costs the lines it
// actually wraps to, not one.
func TestHeaderLinesCountsWrapping(t *testing.T) {
	s := newStyles()
	cw := contentWidth(s, 60)

	cases := []struct {
		header []string
		want   int
	}{
		{[]string{"short", ""}, 2},
		{[]string{strings.Repeat("x", cw)}, 1},   // exactly full: still one line
		{[]string{strings.Repeat("x", cw+1)}, 2}, // one cell over: two
		{[]string{strings.Repeat("x", 2*cw+1)}, 3},
	}
	for _, c := range cases {
		if got := headerLines(s, 60, c.header); got != c.want {
			t.Errorf("headerLines(%d cells) = %d, want %d", lipgloss.Width(c.header[0]), got, c.want)
		}
	}

	// Before the first WindowSizeMsg there is nothing to measure against.
	if got := headerLines(s, 0, []string{strings.Repeat("x", 500)}); got != 1 {
		t.Errorf("unknown width: headerLines = %d, want 1", got)
	}
}
