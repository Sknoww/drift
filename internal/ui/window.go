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

// detailLines is what listBody reserves beneath the rows for the selected row's
// full value (roadmap area 20b).
//
// **It is not folded into listChrome, and that is the point.** listChrome is the
// frame *around* a panel, which the `?` overlay sits inside as much as a list
// does (helpViewportHeight) — but the overlay has no rows and no cursor, so it
// has no detail line, and charging it one would cost the overlay a key row for
// something it never draws. The detail line belongs to the list body, so it is
// costed where it is spent: listBody adds it to the list's own fixed header
// lines, which is exactly what it is.
//
// One line, and so a floor rather than a guarantee. The line gets the panel's
// whole width — ~104 cells at 110 columns, against the ~40 a name column can
// spare — and elides with the same head-weighted rule everything else uses when
// even that is not enough. Wrapping it to two or three would read better on a
// pathological path and cost every list screen those lines on every terminal,
// forever; worse, a detail whose height followed the selected value would make
// the visible row count jump as the cursor moved — the defect the reserve-always
// rule below exists to prevent, reintroduced in the axis it was not looking at.
const detailLines = 1

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
//
// detail is the selected row's full, unstyled value — the branch name, the path,
// the ref. It cannot be derived here: rows arrive already fitted and styled, so
// what a column had to cut is gone by the time this sees it. Pass "" for a
// screen with no such value (the destination overlay's two literals) or for a
// list with no selection.
func listBody(s styles, width, height int, header, rows []string, selected int, detail string) string {
	start, end := rowWindow(len(rows), selected,
		listCapacity(height, headerLines(s, width, header)+detailLines))

	// The selection gutter, blank on every row but the cursor's. It is drawn
	// here rather than by selectBand because every row needs it, not just the
	// selected one: a marker that pushed only its own row right would put that
	// row's columns two cells out of line with the rest of the list. Zero-width
	// for every band treatment, which makes this whole block a no-op there.
	g := s.band.gutter()
	blank := strings.Repeat(" ", g)

	lines := make([]string, 0, len(header)+(end-start)+2)
	lines = append(lines, header...)
	if start > 0 {
		lines = append(lines, blank+s.help.Render(fmt.Sprintf("↑ %d more", start)))
	}

	band := -1
	if selected >= start && selected < end {
		band = len(lines) + (selected - start)
	}
	shown, shownOK := "", false
	for i, row := range rows[start:end] {
		lead := blank
		if g > 0 && start+i == selected {
			lead = s.selMark.Render(bandMarkerGlyph) + strings.Repeat(" ", g-1)
		}
		clipped := clipRow(s, width, row)
		if start+i == selected {
			shown, shownOK = clipped, true
		}
		lines = append(lines, lead+clipped)
	}

	if end < len(rows) {
		lines = append(lines, blank+s.help.Render(fmt.Sprintf("↓ %d more", len(rows)-end)))
	}

	lines = selectBand(s, width, lines, band)
	// After the band, never inside it: the detail is *about* the selected row, not
	// part of it, and painting it with the same background would read as a second
	// selected line. Appended unconditionally — an empty string is the reserved
	// blank, which is the whole of the reserve-always rule.
	lines = append(lines, blank+detailValue(s, width, detail, shown, shownOK))
	return strings.Join(lines, "\n")
}

// detailValue is the detail line's content: the selected row's full value when
// the row could not show it, and nothing when it could.
//
// **Whether it was elided is asked of the rendered row, not predicted.** The
// alternative is for each screen to report what its column budget did, which
// puts the answer a long way from the cut and gets it wrong the first time a
// screen gains a column — and a detail line that disagrees with the row above it
// is worse than none, since a value shown twice reads as two values. Testing the
// drawn row catches every way the value can lose its tail: a column that fitted
// itself, and clipRow cutting a row no budget anticipated.
//
// A row that is not on screen has nothing to be redundant with, so a selection
// outside the window draws no detail rather than one about a row the user cannot
// see — the same rule area 14's filter follows when it hides a selected row.
func detailValue(s styles, width int, detail, shown string, shownOK bool) string {
	if detail == "" || !shownOK || strings.Contains(ansi.Strip(shown), detail) {
		return ""
	}
	if w := rowWidth(s, width); w > 0 {
		detail = elide(detail, w)
	}
	return s.help.Render(detail)
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
// It measures against rowWidth, not contentWidth: inside a list a row spends
// the panel less the selection gutter, and clipping to the panel would let a row
// overflow into the gutter's cells by exactly the gutter's width (view.go).
func clipRow(s styles, width int, row string) string {
	return clipToWidth(rowWidth(s, width), row)
}

// clipPanelLine is clipRow for a line drawn in the panel's place rather than as
// a list row — the help overlay — where there is no cursor and so no selection
// gutter to reserve. It gets the whole content width.
func clipPanelLine(s styles, width int, row string) string {
	return clipToWidth(contentWidth(s, width), row)
}

// clipToWidth is the cut both of the above share. A non-positive width means the
// terminal size is not known yet, and nothing is clipped against a guess.
func clipToWidth(w int, row string) string {
	if w <= 0 {
		return row
	}
	return ansi.Truncate(row, w, "…")
}
