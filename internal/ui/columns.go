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
//
// A third bug lived here and was the mirror image of the first two: the column
// caps were ceilings that never grew, so a wide terminal cut a 56-cell branch
// name to 32 with forty cells sitting empty beside it. They are gone (roadmap
// area 20); a column is now min(its content, what the row has left), and the
// floors below are what a squeeze stops at.

// fit renders s in exactly w display cells: elided when it is wider, padded with
// spaces when it is narrower.
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
	if lipgloss.Width(s) > w {
		s = elide(s, w)
	}
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// elide cuts s down to at most w display cells, keeping as much of the head as
// fits plus the final segment, with `…` between them.
//
// **Which end goes is the whole decision, and this reverses half of 19a's.** A
// tail cut is the obvious rule and it removes exactly the half being read: for a
// path the tail is the identity (`Log4j2Configurer.java`) and the head is
// boilerplate; for a branch name under a `…-to-vscode-mvp3` convention the tail
// is the *target*, which is the half a dashboard row exists to let you check
// against its target column.
//
// 19a rejected a middle-elide and its argument still stands as written — it
// tested `origin/fix/PSOT-22114-…` against `origin/fix/…/mvp-3` and charged the
// second with hiding the half worth reading. Look again at that string: it shows
// `origin/fix/`, which *is* the tell that a feature branch is masquerading as a
// release target. 19a's case assumed an even split or a first-segment-only keep.
// Head-weighted, the elide passes 19a's own test and keeps the target suffix, so
// the package gets one rule rather than two.
//
// Two mechanics, and it is the *tail* they differ over:
//
//   - The whole final segment, when it fits in half the budget:
//     `main-connector/src/main/…/Log4j2Configurer.java`.
//   - A character-level tail of the same share when the last segment *is* the
//     long part. `audit-workflow-migration-to-vscode-mvp3` carries no interior
//     `/`, so a boundary-only rule would degenerate to a tail cut and drop the
//     target — the case this exists for.
//
// The head then fills everything the tail did not take. Keeping only *whole*
// leading segments was the first attempt and it was worse than the tail cut it
// replaced: a ref whose second segment is enormous
// (`origin/fix/PSOT-22114-…-for-audit/mvp-3`) got a head of `origin/fix/` and
// left twenty cells of its budget unspent — 19a's feared string, produced by the
// rule written to avoid it. The head is filled by characters and only *trimmed*
// back to a boundary when what hangs off the end is too short to say anything.
//
// "Head-weighted" is the arithmetic: the tail may never take more than half the
// budget, so what survives is always weighted towards the head that 19a was
// right to protect. The trim is guarded by the same inequality rather than
// allowed to undo it.
func elide(s string, w int) string {
	if w <= 0 {
		return ""
	}
	total := lipgloss.Width(s)
	if total <= w {
		return s
	}
	// Under three cells there is no middle to keep: a head, an ellipsis and a
	// tail do not fit, so fall back to the blunt cut.
	if w < 3 {
		return ansi.Truncate(s, w, "…")
	}

	slashes := slashCells(s)
	maxTail := (w - 1) / 2

	// The final segment carries its own leading `/`, so the elide reads as a path
	// with a hole in it rather than as two strings jammed together.
	tail := maxTail
	if n := len(slashes); n > 0 {
		if seg := total - slashes[n-1]; seg > 0 && seg <= maxTail {
			tail = seg
		}
	}

	head := w - 1 - tail
	// A fragment of a segment is worth keeping when it is long enough to
	// identify something — `PSOT-22114-PickHistor` is the ticket — and is noise
	// when it is not: `…/src/ja…/` reads as a rendering fault. Trim only that
	// second case, and only while the head stays the larger half.
	if p, ok := lastSlashBefore(slashes, head); ok {
		if frag := head - p - 1; frag > 0 && frag < minSegmentFragment && p+1 >= tail {
			head = p + 1
		}
	}

	return ansi.Cut(s, 0, head) + "…" + ansi.Cut(s, total-tail, total)
}

// minSegmentFragment is the shortest trailing piece of a path segment the head
// will end on. Below it the fragment names nothing, and the elide reads better
// cut back to the `/` in front of it.
const minSegmentFragment = 4

// lastSlashBefore is the cell offset of the last `/` strictly inside the first w
// cells of the string those offsets came from.
func lastSlashBefore(slashes []int, w int) (int, bool) {
	for i := len(slashes) - 1; i >= 0; i-- {
		if slashes[i] < w {
			return slashes[i], true
		}
	}
	return 0, false
}

// slashCells is the display-cell offset of every `/` in s, measured on the text
// with its escape sequences stripped so a styled cell reports the positions its
// *rendered* form has — which is what ansi.Cut then slices by.
func slashCells(s string) []int {
	var pos []int
	w := 0
	for _, r := range ansi.Strip(s) {
		if r == '/' {
			pos = append(pos, w)
		}
		w += ansi.StringWidth(string(r))
	}
	return pos
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

// widestCell is the widest of n cells, in display cells. It is deliberately
// unbounded: what bounds a column is the row's own allocation — fixed cost
// first, then the cell carrying the row's point, then the name or path column
// absorbing what is genuinely left. Nothing here caps it.
//
// **The accepted cost, stated so it is not rediscovered as a regression**: one
// long value now sets the column for every row, so short rows carry a ragged gap
// out to the cell beside them. That is what the old caps were buying, and it was
// weighed against a column that lies and lost — a gap is legible, a truncated
// path is not (roadmap area 20). The screens that never had this complaint —
// the targets list, the target picker, the repoint picker — are the ones that
// have always sized this way.
func widestCell(n int, at func(int) string) int {
	w := 0
	for i := 0; i < n; i++ {
		if cw := lipgloss.Width(at(i)); cw > w {
			w = cw
		}
	}
	return w
}

// minNameCol is the floor the name column is never squeezed below. Under it the
// column has stopped saying anything, and the terminal is narrower than
// minTerminalWidth allows anyway — this is the guard, not the mechanism. A floor
// is a different thing from a ceiling, which is why these two outlived the caps.
const minNameCol = 8

// minRefCol is the floor the targets screen's ref column is never squeezed
// below. A ref is longer than a branch name by construction — it carries a
// remote prefix before anything identifying — and the head is the half that
// gives a wrong target away (roadmap 19e), so the column needs room for more
// than `origin/`. Like minNameCol it is the guard, not the mechanism: the key
// column beside it is squeezed against this floor before the ref gives way at
// all.
const minRefCol = 16

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
