package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Chrome — the lines drawn *outside* the panel: the header, the status line and
// the help line (roadmap area 15).
//
// Area 14 bounded how many rows a panel draws, and area 15's first slice gave
// every column its own bound. Neither touches these three, and prose overflows
// exactly as rows do:
//
//   - the dashboard's help line was 108 cells and needed 110, so it wrapped on
//     the near-universal 80-column terminal and still wrapped at 100 — the one
//     line on screen that teaches the keys, wrapping into the panel border;
//   - a git error is worse than wide. It is repo-supplied, unbounded, and
//     *multi-line*, while listChrome costs the status line as exactly one. A
//     two-line error breaks the row budget the same way a wrapping header line
//     did (headerLines, window.go) — the frame runs off the top of the terminal;
//   - the header carries the checked-out branch name, which is as unbounded as
//     any name on a dashboard row.
//
// So the rule the panel already lives by applies out here too: measure against
// the width you actually have, and give way in a stated order.

// chromeWidth is the width available to a line drawn outside the panel. Those
// lines sit inside the app's padding but outside the panel's border, so they
// get more room than contentWidth and must be measured against their own frame.
// Returns 0 before the first WindowSizeMsg — the size is genuinely unknown then,
// and eliding against a guess would drop text a terminal had room for.
func chromeWidth(s styles, width int) int {
	if width == 0 {
		return 0
	}
	w := width - s.app.GetHorizontalFrameSize()
	if w < 1 {
		return 1 // a terminal this narrow is under minTerminalWidth and never drawn
	}
	return w
}

// helpSep is the separator between help segments, and helpElide stands in for
// whatever the width could not hold.
const (
	helpSep   = " · "
	helpElide = "…"
)

// helpLine renders a screen's help as segments elided against the real width.
//
// The alternative was to shorten the text, and it is the worse trade twice
// over: it is lossy at *every* width — a 140-column terminal would show the
// same truncated line as an 80 — and it re-breaks the moment a binding is added
// (areas 11 and 12 both add actions). Eliding is a mechanism; shortening is a
// number that goes stale.
//
// The split is the point. `tail` is what the line must never stop saying — how
// to leave, and where the full key list is — and it is paid for first, exactly
// as a branch row pays for its status cluster before its name (DESIGN.md §1).
// `lead` is spent from the front, so what survives a narrow terminal is the
// start of the line rather than an arbitrary middle, and the elision is marked
// so a shortened line can never read as the whole list.
func helpLine(s styles, width int, lead, tail []string) string {
	full := strings.Join(append(append([]string{}, lead...), tail...), helpSep)

	cw := chromeWidth(s, width)
	if cw <= 0 || lipgloss.Width(full) <= cw {
		return s.help.Render(full)
	}

	// Drop lead segments from the end inward, the marker standing in for them.
	for n := len(lead) - 1; n >= 0; n-- {
		segs := append(append([]string{}, lead[:n]...), helpElide)
		if line := strings.Join(append(segs, tail...), helpSep); lipgloss.Width(line) <= cw {
			return s.help.Render(line)
		}
	}

	// Even the anchors overflow. Clip blind, the same backstop clipRow is for a
	// row: a terminal this narrow is under minTerminalWidth and never drawn.
	return s.help.Render(ansi.Truncate(strings.Join(append([]string{helpElide}, tail...), helpSep), cw, helpElide))
}

// chromeText renders one line of prose into the chrome: whitespace collapsed so
// a multi-line value cannot cost more than the single line the frame budgeted
// for it, then clipped to the width outside the panel.
//
// The collapse is the half that matters. Width is a cosmetic complaint; a git
// error carrying a newline silently spends a line listChrome never counted, and
// the windowed list above it then draws one row too many.
func chromeText(s styles, width int, text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if cw := chromeWidth(s, width); cw > 0 {
		text = ansi.Truncate(text, cw, helpElide)
	}
	return text
}

// panelViewportWidth is the inner panel width a viewport fills, falling back to
// a sane default before the first WindowSizeMsg. Shared by the diff panel and
// the `?` overlay, which sit in the same frame and so have the same width.
func panelViewportWidth(s styles, width int) int {
	if w := contentWidth(s, width); w > 0 {
		return w
	}
	return 80
}
