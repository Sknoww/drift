package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// --- the matcher ------------------------------------------------------------

func TestFilterMatchesAnyShownField(t *testing.T) {
	f, _ := filterState{}.begin()
	f.input.SetValue("PERF")

	cases := []struct {
		key, ref string
		want     bool
	}{
		{"r2perf", "origin/release-to-performance", true}, // matched on the key
		{"r2", "origin/release-perf", true},               // matched on the ref alone
		{"main", "origin/main", false},
	}
	for _, c := range cases {
		if got := f.matches(c.key, c.ref); got != c.want {
			t.Errorf("matches(%q, %q) with query %q = %v, want %v", c.key, c.ref, f.query(), got, c.want)
		}
	}
}

// A query of nothing but whitespace is a typo, not a filter — it must not narrow
// the list to the rows that happen to contain a space.
func TestFilterIgnoresWhitespaceOnlyQueries(t *testing.T) {
	f, _ := filterState{}.begin()
	f.input.SetValue("   ")
	if f.active() {
		t.Error("a whitespace-only query counts as an active filter")
	}
	if !f.matches("anything") {
		t.Error("a whitespace-only query narrowed the list")
	}
}

// Reopening the field resumes the query. esc is how a filter is cleared, and it
// should be the only way — re-entering to refine must never cost the query.
func TestFilterReopenKeepsTheQuery(t *testing.T) {
	f, _ := filterState{}.begin()
	f.input.SetValue("feat")
	f = f.commit()

	f, _ = f.begin()
	if f.query() != "feat" {
		t.Errorf("query after reopening = %q, want it resumed", f.query())
	}
	if got := f.clear(); got.active() || got.open {
		t.Errorf("clear left %+v, want an empty closed filter", got)
	}
}

// cursorFor is what keeps a query change from dumping the user somewhere
// arbitrary: it holds the row when the row survives, and falls to the top only
// when it does not.
func TestCursorForHoldsTheRowWhenItSurvives(t *testing.T) {
	if got := cursorFor([]int{2, 7, 9}, 7); got != 1 {
		t.Errorf("cursorFor = %d, want 1 — the row is still visible", got)
	}
	if got := cursorFor([]int{2, 9}, 7); got != 0 {
		t.Errorf("cursorFor = %d, want 0 — the row is gone, fall to the top", got)
	}
}

// --- the wizard: the screen this exists for ---------------------------------

// wizardWithRefs builds a wizard over generated refs, already sized.
func wizardWithRefs(n int, format string) tea.Model {
	refs := make([]string, n)
	for i := range refs {
		refs[i] = fmt.Sprintf(format, i)
	}
	var m tea.Model = newWizard(refs)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	return m
}

// typeRunes feeds a string in one keystroke at a time, the way a user does — an
// incremental filter has to survive being narrowed a character at a time, not
// just handed a finished query.
func typeRunes(m tea.Model, s string) tea.Model {
	for _, r := range s {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func pressSlash(m tea.Model) tea.Model {
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	return m
}

// The headline case: 400 refs, a query, and the list is down to what the user
// asked for — with the count on screen so a query that found nothing is
// distinguishable from a repo with nothing in it.
func TestWizardFilterNarrowsAndCountsOnScreen(t *testing.T) {
	m := wizardWithRefs(400, "origin/b%04d")
	m = typeRunes(pressSlash(m), "b0033")

	w := m.(wizardModel)
	if got := len(w.visible()); got != 1 {
		t.Fatalf("visible = %d refs, want 1", got)
	}

	view := w.View()
	if !strings.Contains(view, "1 of 400") {
		t.Errorf("the match count is not on screen; got:\n%s", view)
	}
	if !strings.Contains(view, "origin/b0033") {
		t.Errorf("the matching ref is not on screen; got:\n%s", view)
	}
	if strings.Contains(view, "origin/b0034") {
		t.Errorf("a non-matching ref was drawn; got:\n%s", view)
	}
}

// Matching is case-insensitive, which is the whole reason it is a substring
// filter and not a prefix one — nobody types a branch name's capitalisation.
func TestWizardFilterIsCaseInsensitive(t *testing.T) {
	var m tea.Model = newWizard([]string{"origin/TEAM-1234-fix", "origin/main"})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = typeRunes(pressSlash(m), "team")

	if got := len(m.(wizardModel).visible()); got != 1 {
		t.Errorf("visible = %d, want the one TEAM ref matched case-insensitively", got)
	}
}

// A query matching nothing must say so. Silence here reads as "this repo has no
// refs", which is the failure the counts exist to prevent.
func TestWizardFilterSaysWhenNothingMatches(t *testing.T) {
	m := wizardWithRefs(50, "origin/b%04d")
	m = typeRunes(pressSlash(m), "zzz")

	w := m.(wizardModel)
	if _, ok := w.selected(); ok {
		t.Error("a query matching nothing still reports a selected row")
	}

	view := w.View()
	if !strings.Contains(view, "0 of 50") {
		t.Errorf("the zero count is not on screen; got:\n%s", view)
	}
	if !strings.Contains(view, "No ref matches") {
		t.Errorf("an empty result must explain itself; got:\n%s", view)
	}

	// And every verb refuses rather than acting on ref zero.
	out, _ := w.dispatch(ActionToggleCandidate)
	for _, tgt := range out.(wizardModel).targets {
		if tgt.included {
			t.Fatal("space with no visible row selected something")
		}
	}
}

// While the field has focus, the screen's own verbs have to type. `e`, `j` and
// `space` are all bound on this screen and all appear in real branch names.
func TestWizardFilterSwallowsTheScreensVerbs(t *testing.T) {
	var m tea.Model = newWizard([]string{"origin/jekyll spaced", "origin/main"})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = typeRunes(pressSlash(m), "je")

	w := m.(wizardModel)
	if q := w.filter.query(); q != "je" {
		t.Fatalf("query = %q, want %q — a bound key acted instead of typing", q, "je")
	}
	if w.editing {
		t.Error("e opened the rename editor instead of typing into the filter")
	}
	for _, tgt := range w.targets {
		if tgt.included {
			t.Error("a keystroke selected a ref instead of typing into the filter")
		}
	}

	// space types too, rather than toggling the selection. Spelled the way a real
	// terminal sends it: KeySpace *carrying* the rune, so the keymap sees " " and
	// the text field still has something to insert.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if got := m.(wizardModel).filter.input.Value(); got != "je " {
		t.Errorf("value after space = %q, want %q", got, "je ")
	}
}

// A slash is an ordinary character in a query, not a second chance to open the
// field. Ref names are full of them, so this is the domain's normal case rather
// than an edge one.
func TestFilterQueryTakesSlashes(t *testing.T) {
	var m tea.Model = newWizard([]string{"origin/feature/TEAM-1", "origin/hotfix/TEAM-1"})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = typeRunes(pressSlash(m), "feature/TEAM")

	w := m.(wizardModel)
	if w.filter.query() != "feature/TEAM" {
		t.Fatalf("query = %q — a / in the query was swallowed instead of typed", w.filter.query())
	}
	if got := len(w.visible()); got != 1 {
		t.Errorf("visible = %d, want the one feature/ ref", got)
	}
}

// enter accepts the query and hands the keys back, so j/k navigate the narrowed
// list; esc clears it and returns the cursor to the row it was on.
func TestWizardFilterCommitAndClear(t *testing.T) {
	m := wizardWithRefs(400, "origin/b%04d")

	// Move to b0100 first, so clearing has somewhere to return to.
	for i := 0; i < 100; i++ {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}

	m = typeRunes(pressSlash(m), "b01")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // accept

	w := m.(wizardModel)
	if w.filter.open {
		t.Error("enter left the field focused")
	}
	if !w.filter.active() {
		t.Error("enter dropped the query it was meant to accept")
	}
	if idx, _ := w.selected(); w.targets[idx].ref != "origin/b0100" {
		t.Errorf("cursor landed on %q, want it held on origin/b0100 through the narrowing", w.targets[idx].ref)
	}

	// j now moves within the matches, not the whole list.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if idx, _ := m.(wizardModel).selected(); m.(wizardModel).targets[idx].ref != "origin/b0101" {
		t.Errorf("j moved to %q, want the next match", m.(wizardModel).targets[idx].ref)
	}

	// esc clears the filter and leaves the cursor on the same ref.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	w = m.(wizardModel)
	if w.filter.active() || w.filter.open {
		t.Error("esc did not clear the filter")
	}
	if w.declined {
		t.Fatal("esc declined the wizard instead of clearing the filter — one esc, two meanings, wrong one won")
	}
	if idx, _ := w.selected(); w.targets[idx].ref != "origin/b0101" {
		t.Errorf("after clearing, cursor is on %q, want origin/b0101 kept", w.targets[idx].ref)
	}
}

// The rule filtering inherits from windowing, and the one that would silently
// corrupt a save: a ref selected and then filtered out is still selected, still
// saved — and the screen owes the count that says so.
func TestWizardFilterNeverDropsASelection(t *testing.T) {
	m := wizardWithRefs(200, "origin/b%04d")

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace}) // select origin/b0000
	m = typeRunes(pressSlash(m), "b0150")           // filter it out of view

	w := m.(wizardModel)
	if strings.Contains(w.View(), "origin/b0000") {
		t.Fatal("the test is not exercising what it claims: the ref is still on screen")
	}
	if !strings.Contains(w.View(), "1 selected row hidden by the filter") {
		t.Errorf("a hidden selection is not counted on screen; got:\n%s", w.View())
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // accept the query
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // save
	w = m.(wizardModel)
	if !w.done || len(w.result) != 1 || w.result[0].Ref != "origin/b0000" {
		t.Errorf("save = %+v (done %v), want the filtered-out selection kept", w.result, w.done)
	}
}

// A save blocked by a row the query is hiding would name a ref the user cannot
// see. The screen reveals it instead of only reporting it.
func TestWizardBlockedSaveRevealsTheHiddenRef(t *testing.T) {
	// Two refs whose keys collide, so the save blocks on the duplicate.
	var m tea.Model = newWizard([]string{"origin/main", "upstream/main"})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	w := m.(wizardModel)
	for i := range w.targets {
		w.targets[i].included = true
	}
	m = w

	m = typeRunes(pressSlash(m), "upstream") // hide the first of the pair
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // save: blocked on the duplicate

	w = m.(wizardModel)
	if w.done {
		t.Fatal("a duplicate key was saved")
	}
	if w.filter.active() {
		t.Error("the filter still hides the ref the block is about")
	}
	if idx, ok := w.selected(); !ok || w.targets[idx].ref != "upstream/main" {
		t.Errorf("cursor is on target %d, want the ref the notice names", idx)
	}
	if !strings.Contains(w.View(), "upstream/main") {
		t.Errorf("the flagged ref is not on screen; got:\n%s", w.View())
	}
}

// Filtering does not get to undo windowing. The filter line costs a header line,
// and 200 matches still overflow a 24-line terminal — the frame must stay inside
// it at every cursor position, exactly as it does unfiltered.
func TestWizardFilteredFrameStaysBounded(t *testing.T) {
	const height = 24
	m := wizardWithRefs(400, "origin/feature/TEAM-%04d-a-rather-long-branch-name-real-repos-have")
	m = typeRunes(pressSlash(m), "team-01") // TEAM-0100 … TEAM-0199: 100 matches
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	matches := len(m.(wizardModel).visible())
	if matches < 100 {
		t.Fatalf("the filter matched %d refs; the test needs an overflowing result set", matches)
	}
	for i := 0; i < matches; i++ {
		w := m.(wizardModel)
		view := w.View()
		if lines := strings.Count(view, "\n") + 1; lines > height {
			t.Fatalf("match %d: frame is %d lines on a %d-line terminal", i, lines, height)
		}
		if _, ok := w.selected(); !ok {
			t.Fatalf("match %d: nothing selected", i)
		}
		// The ref itself is longer than the panel, so clipRow cuts its tail off by
		// design — the ticket token near the start of the row is what identifies
		// which row is drawn without asserting against the clip.
		if token := fmt.Sprintf("TEAM-%04d", 100+i); !strings.Contains(view, token) {
			t.Fatalf("match %d: the selected ref (%s) is off screen", i, token)
		}
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}
}

// --- the pairing checklist: the same shape, inherited -----------------------

// sizedPairing is the shared pairing fixture with a terminal size, since these
// tests read the rendered frame rather than only the model.
func sizedPairing(t *testing.T, branches ...string) Model {
	t.Helper()
	m := pairingModel(t, "ABC-9", branches...)
	m.width, m.height = 100, 24
	return m
}

func TestPairingFilterNarrowsAndSwallowsTheVerbs(t *testing.T) {
	var m tea.Model = sizedPairing(t, "abc-1-perf", "abc-1-main", "abc-1-spike")
	m = typeRunes(pressSlash(m), "ma") // 'm' and 'a' are both unbound here; 'a' matters on the dashboard

	mm := m.(Model)
	if got := len(mm.add.visible()); got != 1 {
		t.Fatalf("visible = %d candidates, want 1", got)
	}
	if !strings.Contains(mm.View(), "1 of 3") {
		t.Errorf("the match count is not on screen; got:\n%s", mm.View())
	}

	// t opens the target picker unfiltered; while the field has focus it types.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	mm = m.(Model)
	if mm.add.picker {
		t.Error("t opened the picker instead of typing into the filter")
	}
	if mm.add.filter.query() != "mat" {
		t.Errorf("query = %q, want %q", mm.add.filter.query(), "mat")
	}

	// A digit accelerator types too, rather than assigning target 1.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	for _, c := range m.(Model).add.candidates {
		if c.targetKey != "" {
			t.Error("a digit assigned a target instead of typing into the filter")
		}
	}
}

// The same never-drop rule, on the pairing side: a branch included and then
// filtered out is still saved, and a block on one reveals it.
func TestPairingFilterKeepsAndRevealsHiddenSelections(t *testing.T) {
	var m tea.Model = sizedPairing(t, "abc-1-perf", "abc-1-main")

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace}) // include abc-1-perf, no target yet
	m = typeRunes(pressSlash(m), "main")            // hide it

	mm := m.(Model)
	if !strings.Contains(mm.View(), "1 selected row hidden by the filter") {
		t.Errorf("a hidden inclusion is not counted on screen; got:\n%s", mm.View())
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // accept the query
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // save: blocked, abc-1-perf has no target

	mm = m.(Model)
	if mm.screen != screenPairing {
		t.Fatal("an unassigned branch was saved because the filter hid it")
	}
	if !strings.Contains(mm.notice, "abc-1-perf") {
		t.Errorf("notice = %q, want it to name the unassigned branch", mm.notice)
	}
	if mm.add.filter.active() {
		t.Error("the filter still hides the branch the block is about")
	}
	if idx, ok := mm.add.selected(); !ok || mm.add.candidates[idx].branch != "abc-1-perf" {
		t.Error("the cursor is not on the branch the notice names")
	}
}

// esc clears the filter rather than abandoning the add flow — one esc, two
// meanings, and the inner one has to win while the field is focused.
func TestPairingFilterEscClearsRatherThanCancels(t *testing.T) {
	var m tea.Model = sizedPairing(t, "abc-1-perf", "abc-1-main")
	m = typeRunes(pressSlash(m), "perf")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})

	mm := m.(Model)
	if mm.screen != screenPairing {
		t.Fatal("esc left the pairing screen instead of clearing the filter")
	}
	if mm.add.filter.active() || mm.add.filter.open {
		t.Error("esc did not clear the filter")
	}
	if got := len(mm.add.visible()); got != 2 {
		t.Errorf("visible = %d, want the full list restored", got)
	}
}

// The ? table is generated from the live keymap, so / documents itself — the
// property that makes an area-12 rebind free. This is the assertion that the new
// action was wired into the keymap rather than only into the code.
func TestFilterAppearsInTheGeneratedHelp(t *testing.T) {
	m := sizedPairing(t, "abc-1-perf")
	m.showHelp = true

	view := m.View()
	if !strings.Contains(view, "/") || !strings.Contains(view, "narrow the list") {
		t.Errorf("the help table does not document the filter key; got:\n%s", view)
	}
}

func TestFilterKeysBindOnlyTheControlKeys(t *testing.T) {
	k := DefaultFilterKeys()
	want := map[string]Action{
		"up": ActionMoveUp, "down": ActionMoveDown,
		"enter": ActionConfirm, "esc": ActionCancel, "ctrl+c": ActionQuit,
	}
	for key, action := range want {
		if got, ok := k.action(key); !ok || got != action {
			t.Errorf("key %q -> %q, %v; want %q", key, got, ok, action)
		}
	}
	// j and k must stay typeable: they are letters before they are movement.
	for _, key := range []string{"j", "k", " ", "e", "t", "1", "?"} {
		if _, ok := k.action(key); ok {
			t.Errorf("key %q is bound in the filter field — it can never be typed", key)
		}
	}
}
