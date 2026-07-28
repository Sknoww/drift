package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Type-to-filter for list screens (roadmap area 14).
//
// Windowing made a long list *renderable*; it did nothing to make one
// *navigable*. j/k through 418 remote refs is not navigation, and the first-run
// wizard is where that bites: it offers every ref under refs/remotes, unnarrowed,
// and asks the user to find their handful of long-lived mains in it.
//
// Two rules carry over from windowing, and both are the same rule — a list that
// silently hides something is worse than a long one:
//
//   - The match count is always on screen. A query that narrows to nothing has to
//     say so, or it looks like a repo with no refs.
//   - Filtering is a *render* concern, exactly as windowing is (DESIGN.md §1). A
//     row selected and then filtered out is still selected and still saved — so
//     the screen owes a count of how many those are, otherwise the save quietly
//     disagrees with what the user can see.
//
// The matching set is never stored. It is derived from the query on every render,
// for the same reason the window is derived from the cursor rather than tracked
// as scroll state: there is no second copy of the list to keep in sync, and a
// cursor that means "the n-th visible row" cannot go stale against a query that
// has since changed.

// filterState is one list screen's filter: the query, and whether the field
// currently has focus. Screens embed it; the matching itself is theirs, since
// only the screen knows which of a row's fields the user can read.
type filterState struct {
	open  bool // the field has focus: unbound keys type rather than act
	input textinput.Model
}

// query is the live query, trimmed. Leading and trailing spaces are almost
// always a typo rather than intent, and a query of nothing but spaces would
// otherwise match every row while looking like an active filter.
func (f filterState) query() string { return strings.TrimSpace(f.input.Value()) }

// active reports whether a query is narrowing the list. A field open over an
// empty query is not yet filtering anything.
func (f filterState) active() bool { return f.query() != "" }

// begin opens the field. Reopening resumes the previous query rather than
// dropping it — esc is how you clear a filter, and it should be the only way,
// so re-entering to refine a query never costs you the query.
func (f filterState) begin() (filterState, tea.Cmd) {
	ti := textinput.New()
	ti.Prompt = "" // the "/" is drawn by line(), in its own style
	ti.CharLimit = 64
	ti.SetValue(f.input.Value())
	ti.CursorEnd()
	ti.Focus()

	f.input = ti
	f.open = true
	return f, textinput.Blink
}

// commit closes the field and keeps the query, so j/k navigate the narrowed list.
func (f filterState) commit() filterState {
	f.open = false
	return f
}

// clear closes the field and drops the query, restoring the full list.
func (f filterState) clear() filterState { return filterState{} }

// typed feeds a message to the field. Every key the screen's filter keymap does
// not bind lands here — which is what makes an incremental filter possible on a
// screen whose verbs are single letters.
func (f filterState) typed(msg tea.Msg) (filterState, tea.Cmd) {
	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	return f, cmd
}

// matches reports whether any of fields contains the query, case-insensitively.
// Callers pass every field the row actually *shows*, never just the primary one:
// the wizard draws `key ← ref`, so matching one of the two would hide rows the
// user can plainly read the query in.
func (f filterState) matches(fields ...string) bool {
	q := strings.ToLower(f.query())
	if q == "" {
		return true
	}
	for _, s := range fields {
		if strings.Contains(strings.ToLower(s), q) {
			return true
		}
	}
	return false
}

// line is the header line reporting what the filter is doing: the query (as a
// live field while it has focus), how many rows survive it, and how many
// selected rows it is hiding.
//
// The counts are the point of the line, not decoration. `12 of 418` is the
// answer to "did my query find it or is it just not there", and the hidden count
// is the answer to "does the screen still agree with what a save would write".
// The hidden count is drawn in the error style — the same style the pairing
// checklist flags an unassigned branch with — because it means the same kind of
// thing: what is about to happen is not what is on screen.
func (f filterState) line(s styles, shown, total, hiddenSelected int) string {
	field := s.target.Render(f.input.Value())
	if f.open {
		field = f.input.View()
	}

	line := s.help.Render("/") + " " + field + "  " +
		s.hint.Render(fmt.Sprintf("%d of %d", shown, total))
	if hiddenSelected > 0 {
		line += "  " + s.errText.Render(fmt.Sprintf("⚠ %s hidden by the filter", plural(hiddenSelected, "selected row")))
	}
	return line
}

// filterVisible reports the indices of the rows that survive the query, in list
// order. Screens index their own data, so match is a predicate on the index
// rather than a slice of strings — a row's matchable text is often assembled
// from more than one field.
func filterVisible(n int, match func(i int) bool) []int {
	out := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if match(i) {
			out = append(out, i)
		}
	}
	return out
}

// cursorFor places the cursor after the visible set has changed: back on the row
// it was on when that row survived the change, and at the top otherwise.
//
// Both halves matter. Narrowing a query keeps you on the row you were reading
// rather than throwing you to the top of the matches; clearing one returns you
// to where you left off in the full list rather than to the top of 400 refs.
func cursorFor(visible []int, idx int) int {
	for i, v := range visible {
		if v == idx {
			return i
		}
	}
	return 0
}

// hiddenSelectedCount counts rows the user has selected that the query hides.
// The screen owes this number whenever it is non-zero: filtering never drops a
// selection (DESIGN.md §1), so without it the save writes rows the screen is not
// showing.
func hiddenSelectedCount(n int, visible []int, selected func(i int) bool) int {
	shown := make(map[int]bool, len(visible))
	for _, i := range visible {
		shown[i] = true
	}
	count := 0
	for i := 0; i < n; i++ {
		if selected(i) && !shown[i] {
			count++
		}
	}
	return count
}
