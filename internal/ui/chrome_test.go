package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Sknoww/drift/internal/store"
)

// --- the frame, measured whole ---------------------------------------------

// The acceptance test for this slice, and the one that would have caught all
// three bugs at once: whatever the screen is carrying, no line is wider than the
// terminal and no frame is taller than it. Everything below narrows it down to
// which line was at fault.
func TestFrameFitsTheTerminal(t *testing.T) {
	longErr := errors.New("exit status 128: fatal: ambiguous argument " +
		"'origin/release-to-performance...abc-1-perf': unknown revision or path not in the working tree.\n" +
		"Use '--' to separate paths from revisions, like this:\n" +
		"'git <command> [<revision>...] -- [<file>...]'")
	longBranch := strings.Repeat("feature/TEAM-1234-a-very-long-branch-name/", 6)

	for _, size := range []struct{ w, h int }{{60, 24}, {80, 24}, {100, 30}, {120, 40}} {
		for _, sc := range []struct {
			name string
			set  func(m *Model)
		}{
			{"dashboard", func(m *Model) {}},
			{"dashboard+error", func(m *Model) { m.err = longErr }},
			{"dashboard+notice", func(m *Model) { m.notice = strings.Join(strings.Fields(longErr.Error()), " ") }},
			{"dashboard+long branch", func(m *Model) {
				m.current = longBranch
				for _, tk := range m.store.Tickets {
					m.expanded[tk.ID] = true
				}
			}},
			{"help overlay", func(m *Model) { m.showHelp = true }},
			{"help overlay/localonly", func(m *Model) { m.showHelp = true; m.screen = screenLocalOnly }},
			{"pairing", func(m *Model) { m.screen = screenPairing }},
			{"add id", func(m *Model) { m.screen = screenAddID }},
		} {
			m := newModel()
			m.width, m.height = size.w, size.h
			sc.set(&m)

			lines := strings.Split(m.View(), "\n")
			if len(lines) > size.h {
				t.Errorf("%s at %dx%d: frame is %d lines", sc.name, size.w, size.h, len(lines))
			}
			for i, l := range lines {
				if w := lipgloss.Width(l); w > size.w {
					t.Errorf("%s at %dx%d: line %d is %d cells: %q", sc.name, size.w, size.h, i, w, l)
				}
			}
		}
	}
}

// --- the help line ----------------------------------------------------------

// The line that teaches the keys was 108 cells and needed 110, so it wrapped
// into the panel border on an 80-column terminal and still wrapped at 100.
func TestHelpLineFitsAndSaysWhenItElided(t *testing.T) {
	for _, width := range []int{60, 80, 100, 110, 200} {
		m := newModel()
		m.width, m.height = width, 24

		line := m.help()
		avail := width - m.styles.app.GetHorizontalFrameSize()
		if got := lipgloss.Width(line); got > avail {
			t.Errorf("at %d columns the help line is %d cells (%d available): %q", width, got, avail, line)
		}
		// Elision is never silent: a shortened line must not read as the whole list.
		full := strings.Contains(line, "l local")
		if full == strings.Contains(line, helpElide) {
			t.Errorf("at %d columns the line neither shows everything nor marks what it dropped: %q", width, line)
		}
	}
}

// What survives a narrow terminal is chosen, not incidental: the anchors say how
// to leave and where the full key list is, and they are paid for first.
func TestHelpLineKeepsItsAnchors(t *testing.T) {
	for _, width := range []int{60, 80, 100, 200} {
		m := newModel()
		m.width, m.height = width, 24
		line := m.help()
		for _, anchor := range []string{"? help", "q quit"} {
			if !strings.Contains(line, anchor) {
				t.Errorf("at %d columns the help line dropped %q: %q", width, anchor, line)
			}
		}
		// The front of the line survives; the tail of the lead is what goes.
		if !strings.Contains(line, "j/k move") {
			t.Errorf("at %d columns the help line dropped its first segment: %q", width, line)
		}
	}
}

// Every screen's help line, not just the dashboard's: five of them overflowed an
// 80-column terminal, and a new one must not be able to reintroduce the bug.
//
// The frame's own lines are all padded out to its widest, so the help line's
// width is not readable from the rendered frame. What *is* readable, and is the
// property that matters, is that the whole line is still on one line: its last
// segment must land on the same frame line as its first. A wrapped line puts
// them on two, and TestFrameFitsTheTerminal catches the width alongside it.
func TestEveryScreensHelpLineFits(t *testing.T) {
	for _, width := range []int{60, 80, 100} {
		for _, sc := range []struct {
			name, first, last string
			set               func(m *Model)
		}{
			{"dashboard", "j/k move", "q quit", func(m *Model) {}},
			{"confirm delete", "y confirm", "n cancel", func(m *Model) { m.screen = screenConfirmDelete }},
			{"add id", "enter continue", "esc cancel", func(m *Model) { m.screen = screenAddID }},
			{"pairing", "space toggle", "esc cancel", func(m *Model) { m.screen = screenPairing }},
			{"pairing/filtering", "type to filter", "esc clear", func(m *Model) {
				m.screen = screenPairing
				m.add.filter.open = true
			}},
			{"picker", "j/k move", "esc back", func(m *Model) { m.screen = screenPairing; m.add.picker = true }},
			{"local-only", "j/k move", "q quit", func(m *Model) { m.screen = screenLocalOnly }},
			{"shelve", "esc back", "q quit", func(m *Model) { m.screen = screenShelve }},
			{"help overlay", "j/k scroll", "any other key closes", func(m *Model) { m.showHelp = true }},
		} {
			m := newModel()
			m.width, m.height = width, 24
			sc.set(&m)

			lines := strings.Split(m.View(), "\n")
			help := lines[len(lines)-1]
			if !strings.Contains(help, sc.first) || !strings.Contains(help, sc.last) {
				t.Errorf("%s at %d columns: help line wrapped or lost an anchor (want %q…%q): %q",
					sc.name, width, sc.first, sc.last, help)
			}
		}
	}
}

// The wizard runs as its own Bubble Tea program with its own frame, so it has
// to be measured separately — and it is the screen a first run lands on.
func TestWizardFrameFitsTheTerminal(t *testing.T) {
	refs := make([]string, 400)
	for i := range refs {
		refs[i] = fmt.Sprintf("origin/feature/TEAM-%04d-a-rather-long-branch-name", i)
	}
	for _, width := range []int{minTerminalWidth, 80, 100} {
		var m tea.Model = newWizard(remoteBranches(refs...), store.Prefs{})
		m, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		w := m.(wizardModel)
		w.notice = "pick at least one target — origin/feature/TEAM-0000-a-rather-long-branch-name was not selected"
		m = w

		lines := strings.Split(m.(wizardModel).View(), "\n")
		for i, l := range lines {
			if got := lipgloss.Width(l); got > width {
				t.Errorf("wizard at %d columns: line %d is %d cells: %q", width, i, got, l)
			}
		}
		if help := lines[len(lines)-1]; !strings.Contains(help, "j/k move") || !strings.Contains(help, "esc skip") {
			t.Errorf("wizard at %d columns: help line wrapped or lost an anchor: %q", width, help)
		}
	}
}

// A filter applied makes esc mean something else, and the line says which is
// live (DESIGN.md §3) — so that segment is an anchor and elision can never eat it.
func TestFilterHelpKeepsTheEscMeaning(t *testing.T) {
	m := newModel()
	m.width, m.height = 60, 24
	m.screen = screenPairing
	f, _ := m.add.filter.begin()
	f.input.SetValue("abc")
	m.add.filter = f.commit()

	lines := strings.Split(m.View(), "\n")
	if last := lines[len(lines)-1]; !strings.Contains(last, "esc clear filter") {
		t.Errorf("the narrow pairing help line dropped what esc now means: %q", last)
	}
}

// --- the status line --------------------------------------------------------

// A git error is unbounded and multi-line, while the frame budgets this line as
// exactly one. The width was the cosmetic half; the newline was the bug.
func TestStatusLineIsAlwaysOneLine(t *testing.T) {
	m := newModel()
	m.width, m.height = 80, 24
	m.err = errors.New("exit status 128: fatal: bad revision\nUse '--' to separate paths\nfrom revisions")

	line := m.statusLine()
	if strings.Contains(line, "\n") {
		t.Errorf("a multi-line error rendered as %d lines: %q", strings.Count(line, "\n")+1, line)
	}
	if got, avail := lipgloss.Width(line), 78; got > avail {
		t.Errorf("status line is %d cells, %d available: %q", got, avail, line)
	}
	// The head of the message is what survives — it is where git says what failed.
	if !strings.Contains(line, "error: exit status 128") {
		t.Errorf("status line lost the head of the error: %q", line)
	}
}

// The same contract for a notice, which carries a fetch error verbatim.
func TestNoticeIsAlwaysOneLine(t *testing.T) {
	m := newModel()
	m.width, m.height = 80, 24
	m.notice = "fetch failed — showing last-known status: exit status 128\nfatal: could not read Username"

	if line := m.statusLine(); strings.Contains(line, "\n") {
		t.Errorf("a multi-line notice rendered as %d lines: %q", strings.Count(line, "\n")+1, line)
	}
}

// Before the first WindowSizeMsg the size is genuinely unknown, so nothing is
// clipped against a guess — the rule contentWidth already follows. The newline
// collapse is not a width decision and applies regardless.
func TestChromeDoesNotClipAgainstAnUnknownWidth(t *testing.T) {
	s := newStyles(store.Prefs{})
	long := strings.Repeat("x", 300)
	if got := chromeText(s, 0, long); got != long {
		t.Errorf("chromeText clipped against an unknown width: %d cells", lipgloss.Width(got))
	}
	if got := chromeText(s, 0, "a\nb"); got != "a b" {
		t.Errorf("chromeText did not collapse a newline at unknown width: %q", got)
	}
}

// --- the header -------------------------------------------------------------

// The header carries the checked-out branch, which is as unbounded as any name
// on a row below it. It absorbs what the title leaves rather than wrapping.
func TestHeaderAbsorbsALongBranchName(t *testing.T) {
	m := newModel()
	m.width, m.height = 80, 24
	m.current = strings.Repeat("feature/TEAM-1234-long/", 10)

	header := m.header()
	if strings.Contains(header, "\n") {
		t.Fatalf("header rendered as %d lines", strings.Count(header, "\n")+1)
	}
	if got, avail := lipgloss.Width(header), 78; got > avail {
		t.Errorf("header is %d cells, %d available", got, avail)
	}
	if !strings.Contains(header, "drift") {
		t.Errorf("the header lost its title to the branch name: %q", header)
	}
}

// --- the ? overlay ----------------------------------------------------------

// It was 28 lines on the dashboard against a 24-line terminal, so the keys it
// exists to teach were the ones scrolled off the top.
func TestHelpOverlayFitsEveryScreen(t *testing.T) {
	for _, sc := range []struct {
		name string
		set  func(m *Model)
	}{
		{"dashboard", func(m *Model) {}},
		{"pairing", func(m *Model) { m.screen = screenPairing }},
		{"diff", func(m *Model) { m.screen = screenDiff }},
		{"local-only", func(m *Model) { m.screen = screenLocalOnly }},
		{"shelve", func(m *Model) { m.screen = screenShelve }},
	} {
		for _, size := range []struct{ w, h int }{{60, 24}, {80, 24}, {120, 30}} {
			m := newModel()
			m.width, m.height = size.w, size.h
			m.showHelp = true
			sc.set(&m)

			if got := len(strings.Split(m.View(), "\n")); got > size.h {
				t.Errorf("? on %s at %dx%d: %d lines", sc.name, size.w, size.h, got)
			}
		}
	}
}

// Scrolling exists so nothing is unreachable: every row of the table must be
// on screen at some offset.
func TestHelpOverlayScrollsToEveryRow(t *testing.T) {
	m := newModel()
	m.width, m.height = 80, 24
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = next.(Model)

	if !m.helpScrolls() {
		t.Fatal("the dashboard overlay is expected to overflow an 80x24 terminal")
	}
	// The last row of the glyph legend is the one that was off the bottom.
	const last = "both sides changed a file git can't merge"
	if strings.Contains(m.View(), last) {
		t.Fatal("fixture no longer overflows; the test proves nothing")
	}
	for i := 0; i < 40 && !strings.Contains(m.View(), last); i++ {
		next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		m = next.(Model)
		if !m.showHelp {
			t.Fatal("j closed the overlay instead of scrolling it")
		}
	}
	if !strings.Contains(m.View(), last) {
		t.Errorf("the last legend row is unreachable by scrolling:\n%s", m.View())
	}
}

// The carve-out is exactly the scroll keys, and only while there is something to
// scroll. Everything else still closes — above all `d`, which the viewport's own
// keymap would have swallowed as half-page-down while the dashboard means delete.
func TestHelpOverlayClosesOnEveryKeyButItsScrollKeys(t *testing.T) {
	open := func() Model {
		m := newModel()
		m.width, m.height = 80, 24
		next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
		return next.(Model)
	}

	for _, key := range []string{"d", "u", "f", "b", " ", "h", "l", "r", "a", "enter", "esc"} {
		next, _ := open().handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if next.(Model).showHelp {
			t.Errorf("%q did not close the overlay", key)
		}
	}
	for _, key := range []string{"j", "k"} {
		next, _ := open().handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if !next.(Model).showHelp {
			t.Errorf("%q closed the overlay instead of scrolling it", key)
		}
	}
}

// When the overlay fits, "any key closes" holds unqualified — the footer must
// not promise a scroll the screen cannot do.
func TestHelpOverlayThatFitsClosesOnItsScrollKeys(t *testing.T) {
	m := newModel()
	m.width, m.height = 80, 60 // tall enough to hold the whole table
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = next.(Model)

	if m.helpScrolls() {
		t.Fatal("the overlay is expected to fit a 60-line terminal")
	}
	if got := m.View(); !strings.Contains(got, "any key closes") || strings.Contains(got, "j/k scroll") {
		t.Errorf("a fitting overlay claims the wrong contract:\n%s", got)
	}
	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if next.(Model).showHelp {
		t.Error("j did not close an overlay that fits")
	}
}

// The pane is derived from the size on every render, so a resize refits it with
// no wiring — and the offset can never point past content it was measured
// against (DESIGN.md §1: derived, never tracked).
func TestHelpOverlaySurvivesAResize(t *testing.T) {
	m := newModel()
	m.width, m.height = 80, 24
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = next.(Model)
	for i := 0; i < 10; i++ {
		next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		m = next.(Model)
	}

	grown, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 60})
	got := grown.(Model)
	if lines := strings.Split(got.View(), "\n"); len(lines) > 60 {
		t.Errorf("after growing, the overlay is %d lines", len(lines))
	}
	// Everything fits now, so nothing is left below the fold.
	if got.helpScrolls() {
		t.Error("the overlay still reports as scrolling on a 60-line terminal")
	}
	if !strings.Contains(got.View(), "Keys — dashboard") {
		t.Errorf("a stale offset scrolled the title off a terminal that fits it:\n%s", got.View())
	}
}

// Each opening starts at the top: the last visit's offset is not this one's.
func TestHelpOverlayOpensAtTheTop(t *testing.T) {
	m := newModel()
	m.width, m.height = 80, 24
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = next.(Model)
	for i := 0; i < 5; i++ {
		next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		m = next.(Model)
	}
	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}) // close
	m = next.(Model)
	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}}) // reopen
	m = next.(Model)

	if m.helpOffset != 0 {
		t.Errorf("reopening the overlay resumed at offset %d", m.helpOffset)
	}
	if !strings.Contains(m.View(), "Keys — dashboard") {
		t.Errorf("the reopened overlay does not start at the top:\n%s", m.View())
	}
}
