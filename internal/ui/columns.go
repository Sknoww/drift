package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Column sizing — every cell bounds itself, and every width is measured in
// display cells (roadmap area 15).
//
// Two bugs lived here, and they were the same bug wearing two hats. The package
// had two padding helpers: one measured with lipgloss.Width, the other with
// len(). Neither truncated, so a column was only ever capped against
// *over*-padding — a 60-character branch name in a column "capped" at 32
// rendered all 60 and shoved the status cluster off the right-hand end, where
// area 14's clipRow then cut it. The branch name ate the row and the signals it
// was meant to sit beside were what got dropped.
//
// fit is the answer to both: one helper, display-width measured, that renders a
// cell in exactly the width its column was given. clipRow is still underneath as
// the backstop — it bounds the assembled row — but it is a floor, not the
// mechanism. A column that sizes itself never reaches it.

// fit renders s in exactly w display cells: truncated with an ellipsis when it
// is wider, padded with spaces when it is narrower.
//
// Display width, not byte length: a branch name can carry a non-ASCII or
// double-width rune, and len() would misalign every column on the row. The cut
// is ANSI-aware for the same reason clipRow's is — some cells arrive already
// styled (the help overlay's glyph legend), and slicing one by bytes would sever
// an escape sequence and bleed its color down the frame.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	switch sw := lipgloss.Width(s); {
	case sw > w:
		return ansi.Truncate(s, w, "…")
	case sw < w:
		return s + strings.Repeat(" ", w-sw)
	default:
		return s
	}
}

// padLeft right-aligns s in w display cells, so a column of ages lines up on its
// unit rather than on its first digit. It does not truncate: its one caller
// (the wizard's age column) is fixed-width by construction and sized to the
// longest value it can emit.
func padLeft(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

// widestCell is the widest of n cells in display cells, bounded by max. A max of
// 0 means unbounded — for a column whose content is short by construction.
func widestCell(n, max int, at func(int) string) int {
	w := 0
	for i := 0; i < n; i++ {
		if cw := lipgloss.Width(at(i)); cw > w {
			w = cw
		}
	}
	if max > 0 && w > max {
		return max
	}
	return w
}

// Column caps. Each bounds a column whose content is user- or repo-supplied and
// therefore unbounded in length; the cap is what stops one long value from
// setting the width of every row. They are ceilings, not fixed widths — a column
// whose content is shorter still shrinks to fit, and one squeezed by a narrow
// terminal shrinks below the cap.
const (
	maxNameCol    = 32 // a branch name on the dashboard and the pairing checklist
	maxTargetCol  = 20 // a Target.Key: terse by intent, but nothing enforces it
	maxKeyCol     = 24 // the wizard's editable key column
	maxPathCol    = 48 // a held path in the local-only manager
	maxPatternCol = 40 // a glob or file path in the declare overlay
)

// minNameCol is the floor the name column is never squeezed below. Under it the
// column has stopped saying anything, and the terminal is narrower than
// minTerminalWidth allows anyway — this is the guard, not the mechanism.
const minNameCol = 8

// minTerminalWidth is the narrowest terminal Drift will draw into. Below it a
// dashboard row cannot hold a branch name beside its status cluster, and
// rendering one anyway produces garbage rather than a compressed view — so the
// screen says it is too narrow instead of pretending.
//
// Sized from the row it has to fit: at 60 columns the panel's content width is
// 54, which leaves the name column ~27 cells beside a full cluster. It sits well
// clear of the near-universal 80-column default, so it only ever fires on a
// deliberately squeezed pane.
const minTerminalWidth = 60

// tooNarrowView is the whole frame when the terminal is under minTerminalWidth,
// shared by the dashboard and the first-run wizard so a squeezed pane says the
// same thing wherever the user is.
//
// It reports false before the first WindowSizeMsg: the size is genuinely unknown
// then, and refusing to draw on a guess would be worse than drawing.
func tooNarrowView(s styles, width int) (string, bool) {
	if width <= 0 || width >= minTerminalWidth {
		return "", false
	}
	lines := []string{
		s.title.Render("drift"),
		"",
		s.errText.Render(fmt.Sprintf("too narrow: %d columns needed, %d here", minTerminalWidth, width)),
		s.help.Render("widen the window · q quits"),
	}
	// Clipped to what the caller's own frame leaves, so the message about not
	// fitting cannot itself wrap. Callers render this through styles.app, whose
	// padding comes out of the same width.
	avail := width - s.app.GetHorizontalFrameSize()
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, avail, "")
	}
	return strings.Join(lines, "\n"), true
}
