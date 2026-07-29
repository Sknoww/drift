package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Windowing — render only the rows that fit, around the cursor (roadmap area 14).
//
// Every list screen used to render every row it had. That is linear work in Go
// and fine at small counts, but Bubble Tea rewrites the *whole* frame on every
// keystroke: a repo with a few thousand remote refs produces a megabyte-plus
// frame per press, and the terminal drowning in that ANSI presents as a hard
// freeze rather than as lag. Windowing bounds the frame at the terminal height
// whatever the row count, which is why it is the fix and not merely an
// optimisation — filtering to a couple of hundred matches would still overflow.
//
// The window is derived from the cursor rather than tracked as scroll state.
// There is nothing to keep in sync and nothing to reset when a list is rebuilt,
// and the invariant that matters — the cursor is always drawn — holds by
// construction rather than by remembering to clamp an offset.

// listChrome is the number of terminal lines the shared frame occupies around a
// list panel's rows: the header row, the panel's top and bottom border, the
// status line, and the help line (screenView, and the wizard's own View, which
// has the same shape). The status line is counted even when nothing is showing,
// so the visible row count does not jump as a notice comes and goes — and a
// screen without one simply keeps a spare line at the bottom.
const listChrome = 5

// listCapacity is how many row lines a list panel can draw: the terminal height
// less the shared chrome and the list's own fixed header lines. It reports 0
// before the first WindowSizeMsg — the size is genuinely unknown then, and
// clipping to a guess would hide rows on a terminal that could show them.
func listCapacity(height, headerLines int) int {
	if height <= 0 {
		return 0
	}
	c := height - listChrome - headerLines
	if c < 1 {
		return 1 // absurdly short terminal: still draw the cursor's row
	}
	return c
}

// rowWindow is the half-open range of rows to draw, chosen so the cursor is
// always inside it. capacity <= 0 means "size unknown" and draws everything.
//
// An overflowing list carries a "more" marker at each clipped edge, and each
// marker costs a line of the same budget the rows come out of. Both are
// reserved up front and the line handed back to the window at whichever edge
// turns out to be flush, so a list scrolled to its top or bottom shows one more
// row rather than leaving a line unspent.
func rowWindow(n, selected, capacity int) (start, end int) {
	if capacity <= 0 || n <= capacity {
		return 0, n
	}
	if selected < 0 {
		selected = 0
	}
	if selected >= n {
		selected = n - 1
	}

	size := capacity - 2
	if size < 1 {
		size = 1
	}

	start = selected - size/2 // centre the cursor, then pull inside both ends
	if start > n-size {
		start = n - size
	}
	if start < 0 {
		start = 0
	}
	end = start + size

	// A flush edge needs no marker; spend its reserved line on a row instead.
	if start == 0 && end < n {
		end++
	} else if end == n && start > 0 {
		start--
	}
	return start, end
}

// listBody assembles a list panel: the fixed header lines, then the rows —
// windowed to what the terminal can hold, marked at any clipped edge, and
// banded at the cursor. Every list screen goes through here, so windowing is
// one implementation rather than five (the diff panel is the exception and
// always was: it windows through a viewport, because it scrolls free text
// rather than selectable rows).
//
// selected indexes rows, not the assembled lines; pass -1 for a list with no
// active selection. The band is re-derived against the window, so it lands on
// the right line however far down the list the cursor is.
func listBody(s styles, width, height int, header, rows []string, selected int) string {
	start, end := rowWindow(len(rows), selected, listCapacity(height, headerLines(s, width, header)))

	lines := make([]string, 0, len(header)+(end-start)+2)
	lines = append(lines, header...)
	if start > 0 {
		lines = append(lines, s.help.Render(fmt.Sprintf("↑ %d more", start)))
	}

	band := -1
	if selected >= start && selected < end {
		band = len(lines) + (selected - start)
	}
	for _, row := range rows[start:end] {
		lines = append(lines, clipRow(s, width, row))
	}

	if end < len(rows) {
		lines = append(lines, s.help.Render(fmt.Sprintf("↓ %d more", len(rows)-end)))
	}
	return strings.Join(selectBand(s, width, lines, band), "\n")
}

// headerLines is what a list's fixed header really costs in terminal lines.
//
// listCapacity subtracts the header from the row budget, and every caller used
// to pass len(header) — which is right only while every header line fits. A
// header line wider than the panel wraps to two, so the list draws one row more
// than the terminal can hold and the frame runs off the top: exactly the failure
// windowing exists to prevent, reintroduced by prose rather than by rows. Found
// by measuring the wizard at minTerminalWidth, where its two-line intro wraps to
// three (roadmap area 15).
//
// The rows themselves need no such arithmetic — clipRow already caps each at one
// line. Shortening the prose so it stops wrapping at all is a separate job; this
// makes the budget honest either way.
func headerLines(s styles, width int, header []string) int {
	cw := contentWidth(s, width)
	if cw <= 0 {
		return len(header) // size unknown: nothing to measure against
	}
	n := 0
	for _, h := range header {
		w := lipgloss.Width(h)
		if w <= cw {
			n++
			continue
		}
		n += (w + cw - 1) / cw // ceiling division: the lines it wraps to
	}
	return n
}

// clipRow caps a row at the panel's content width.
//
// Windowing bounds how many rows are drawn; this bounds how many *lines* each
// one costs, and without it the budget is fiction. A window of 19 rows that each
// wrap is 38 lines on a 24-line terminal: the frame runs off the top and the
// cursor goes with it, which is the very failure windowing was built to stop.
//
// The cut is ANSI-aware — a row is assembled from styled cells, and slicing one
// by bytes would sever an escape sequence and bleed its color down the frame.
//
// It is the **backstop**, not the mechanism. Area 15 gave every column its own
// bound (columns.go), so a row that reaches here at all is one no column budget
// anticipated — a trailing cell with nothing after it to align, or a screen
// still to be converted. Clipping blind drops whatever overflows off the
// right-hand end; a column that sizes itself ellipsises in place instead.
func clipRow(s styles, width int, row string) string {
	cw := contentWidth(s, width)
	if cw <= 0 {
		return row // size unknown: nothing to clip against
	}
	return ansi.Truncate(row, cw, "…")
}
