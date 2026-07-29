package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Sknoww/drift/internal/git"
)

// The wizard's git (listing remote refs) and IO (writing config) live in the
// caller; these tests drive the selection logic that decides what gets written.

func wizardWith(refs ...string) wizardModel {
	return newWizard(remoteBranches(refs...))
}

// remoteBranches wraps bare ref names as the branch list the wizard is built
// over. The dates are left zero: they drive only the age column, so every test
// that is about selection, filtering or windowing wants them out of the way.
// The ones that are about the column set them explicitly.
func remoteBranches(refs ...string) []git.RemoteBranch {
	out := make([]git.RemoteBranch, len(refs))
	for i, ref := range refs {
		out[i] = git.RemoteBranch{Ref: ref}
	}
	return out
}

func TestDeriveKeyStripsRemote(t *testing.T) {
	cases := map[string]string{
		"origin/main":         "main",
		"origin/release-perf": "release-perf",
		"origin/feature/x":    "feature/x", // only the remote is stripped, not the whole path
		"upstream/main":       "main",
	}
	for ref, want := range cases {
		if got := deriveKey(ref); got != want {
			t.Errorf("deriveKey(%q) = %q, want %q", ref, got, want)
		}
	}
}

func TestNewWizardSeedsEditableKeys(t *testing.T) {
	m := wizardWith("origin/main", "origin/release-perf")
	if len(m.targets) != 2 {
		t.Fatalf("targets = %d, want 2", len(m.targets))
	}
	if m.targets[0].key != "main" || m.targets[0].ref != "origin/main" {
		t.Errorf("target[0] = %+v, want key=main ref=origin/main", m.targets[0])
	}
	if m.targets[0].included {
		t.Error("a freshly offered ref must start unselected — the wizard never guesses")
	}
}

func TestWizardDefaultKeysShareThePairingShape(t *testing.T) {
	k := DefaultWizardKeys()
	want := map[string]Action{
		"j": ActionMoveDown, "k": ActionMoveUp,
		" ": ActionToggleCandidate, "e": ActionEditKey,
		"enter": ActionConfirm, "esc": ActionCancel, "ctrl+c": ActionQuit,
	}
	for key, action := range want {
		if got, ok := k.action(key); !ok || got != action {
			t.Errorf("key %q -> %q, %v; want %q", key, got, ok, action)
		}
	}
}

func TestWizardSelectAndSave(t *testing.T) {
	m := wizardWith("origin/main", "origin/release-perf")

	// Select the second ref (cursor down, then space).
	next, _ := m.dispatch(ActionMoveDown)
	next, _ = next.(wizardModel).dispatch(ActionToggleCandidate)
	m = next.(wizardModel)

	out, cmd := m.dispatch(ActionConfirm)
	final := out.(wizardModel)
	if !final.done || final.declined {
		t.Fatalf("save with one selection: done=%v declined=%v, want done", final.done, final.declined)
	}
	if len(final.result) != 1 || final.result[0].Key != "release-perf" || final.result[0].Ref != "origin/release-perf" {
		t.Errorf("result = %+v, want the one selected target", final.result)
	}
	if !yieldsQuit(cmd) {
		t.Error("a completed save must quit the wizard program")
	}
}

func TestWizardSaveWithNothingSelected(t *testing.T) {
	// Zero targets fails store validation, so the wizard blocks it here with a
	// pointer rather than letting the save fail downstream.
	m := wizardWith("origin/main")
	out, cmd := m.dispatch(ActionConfirm)
	final := out.(wizardModel)
	if final.done {
		t.Error("save with nothing selected marked done, want blocked")
	}
	if cmd != nil {
		t.Error("blocked save must not quit")
	}
	if final.notice == "" {
		t.Error("blocked save must explain why nothing was written")
	}
}

func TestWizardBlocksDuplicateKeys(t *testing.T) {
	// Two remotes with a same-named branch both default to the same key;
	// duplicate keys are ambiguous, so the save is blocked until one is renamed.
	m := wizardWith("origin/main", "upstream/main")
	for i := range m.targets {
		m.targets[i].included = true
	}
	out, cmd := m.dispatch(ActionConfirm)
	final := out.(wizardModel)
	if final.done || cmd != nil {
		t.Fatal("save with duplicate keys must be blocked")
	}
	if !strings.Contains(final.notice, "duplicate key") {
		t.Errorf("notice = %q, want it to flag the duplicate", final.notice)
	}
}

func TestWizardRenameKey(t *testing.T) {
	m := wizardWith("origin/release-to-performance")

	out, _ := m.dispatch(ActionEditKey)
	edit := out.(wizardModel)
	if !edit.editing {
		t.Fatal("edit_key did not open the editor")
	}
	if !edit.targets[0].included {
		t.Error("renaming a ref should include it")
	}

	// Type a terser key and commit.
	edit.input.SetValue("r2perf")
	renamed := edit.commitEdit()
	if renamed.editing {
		t.Error("commit should close the editor")
	}
	if renamed.targets[0].key != "r2perf" {
		t.Errorf("key after rename = %q, want r2perf", renamed.targets[0].key)
	}

	out, _ = renamed.dispatch(ActionConfirm)
	final := out.(wizardModel)
	if len(final.result) != 1 || final.result[0].Key != "r2perf" {
		t.Errorf("saved result = %+v, want the renamed key", final.result)
	}
}

func TestWizardEmptyRenameIsRejected(t *testing.T) {
	m := wizardWith("origin/main")
	out, _ := m.dispatch(ActionEditKey)
	edit := out.(wizardModel)
	edit.input.SetValue("   ")
	after := edit.commitEdit()
	if !after.editing {
		t.Error("an empty rename must keep the editor open")
	}
	if after.targets[0].key != "main" {
		t.Errorf("key = %q, want the original kept", after.targets[0].key)
	}
}

func TestWizardDeclineFallsBack(t *testing.T) {
	// esc backs out to the hand-edit path: declined, not done, and quits.
	m := wizardWith("origin/main")
	out, cmd := m.dispatch(ActionCancel)
	final := out.(wizardModel)
	if !final.declined || final.done {
		t.Errorf("cancel: declined=%v done=%v, want declined", final.declined, final.done)
	}
	if !yieldsQuit(cmd) {
		t.Error("cancel must quit the wizard program")
	}
}

// TestWizardThroughUpdateAndView drives the wizard the way the runtime does —
// real KeyMsgs through Update, then View — so the handleKey routing and the
// render path are covered, not just dispatch in isolation.
func TestWizardThroughUpdateAndView(t *testing.T) {
	var m tea.Model = wizardWith("origin/main", "origin/release-perf")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// The wizard screen shows both refs, unchecked, with the seeded keys.
	view := m.(wizardModel).View()
	for _, want := range []string{"first-run setup", "[ ]", "origin/main", "release-perf"} {
		if !strings.Contains(view, want) {
			t.Errorf("wizard view missing %q:\n%s", want, view)
		}
	}

	// Space selects the first ref; the box flips to [x].
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if !strings.Contains(m.(wizardModel).View(), "[x]") {
		t.Error("space did not check the selected ref in the rendered view")
	}

	// Enter saves and quits with the one selected target.
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	final := m.(wizardModel)
	if !final.done || len(final.result) != 1 || final.result[0].Ref != "origin/main" {
		t.Fatalf("after enter: done=%v result=%+v, want the one checked target", final.done, final.result)
	}
	if !yieldsQuit(cmd) {
		t.Error("save must quit the program")
	}
}

func yieldsQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// Recency ordering and the age column (roadmap area 14's last open question).
//
// The decision the area closes on is that recency is an *ordering* and never a
// narrowing: it moves the likely mains up and removes nothing, so a repo whose
// main is dormant still lists it. These pin both halves — the order the wizard
// preserves, and the column that keeps that order from reading as arbitrary.

func TestRelativeAge(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{30 * time.Second, "now"},
		{-time.Hour, "now"}, // a tip dated in the future: clock skew, not a negative age
		{5 * time.Minute, "5m"},
		{3 * time.Hour, "3h"},
		{25 * time.Hour, "1d"},
		{29 * 24 * time.Hour, "29d"},
		{30 * 24 * time.Hour, "1mo"},
		{364 * 24 * time.Hour, "12mo"},
		{2 * 365 * 24 * time.Hour, "2y"},
	}
	for _, c := range cases {
		got := relativeAge(now.Add(-c.ago), now)
		if got != c.want {
			t.Errorf("relativeAge(-%s) = %q, want %q", c.ago, got, c.want)
		}
		// The column is fixed-width by construction; a value that outgrew it
		// would push the ref name off the panel on every row.
		if len(got) > ageColWidth {
			t.Errorf("relativeAge(-%s) = %q, wider than the %d-column cell", c.ago, got, ageColWidth)
		}
	}
}

func TestRelativeAgeUnknownDateRendersEmpty(t *testing.T) {
	// git reported no usable committerdate. The column says nothing rather than
	// inventing an age — the one thing it exists to state.
	if got := relativeAge(time.Time{}, time.Now()); got != "" {
		t.Errorf("relativeAge(zero) = %q, want empty", got)
	}
}

func TestWizardPreservesTheOrderItIsGiven(t *testing.T) {
	// git sorts by recency (git.RemoteBranches); the wizard must not re-sort.
	// Alphabetically this list is exactly backwards, so any tidying shows up.
	m := newWizard([]git.RemoteBranch{
		{Ref: "origin/release/r2-perf"},
		{Ref: "origin/main"},
		{Ref: "origin/abandoned"},
	})
	want := []string{"origin/release/r2-perf", "origin/main", "origin/abandoned"}
	for i, ref := range want {
		if m.targets[i].ref != ref {
			t.Fatalf("target[%d] = %q, want %q — the wizard re-sorted a list git had already ordered",
				i, m.targets[i].ref, ref)
		}
	}
}

func TestWizardRendersTheAgeColumn(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	m := newWizard([]git.RemoteBranch{
		{Ref: "origin/main", Updated: now.Add(-2 * 24 * time.Hour)},
		{Ref: "origin/dormant", Updated: now.Add(-400 * 24 * time.Hour)},
	})
	m.now = now
	m.width, m.height = 100, 24

	view := m.View()
	for _, want := range []string{"2d", "1y"} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing the age %q:\n%s", want, view)
		}
	}
	// The age leads its row, so clipping can never eat it and the column always
	// aligns. Checking the ordering within the line rather than exact spacing:
	// the styles wrap each cell in escapes.
	line := lineContaining(t, view, "origin/main")
	if strings.Index(line, "2d") > strings.Index(line, "main") {
		t.Errorf("age must lead the row, not trail it: %q", line)
	}
}

func TestWizardUnknownAgeStillRendersTheRow(t *testing.T) {
	// A ref with no usable date loses its age cell, never its row — the ref is
	// the load-bearing half and first-run setup must still offer it.
	m := wizardWith("origin/main", "origin/release-perf")
	m.width, m.height = 100, 24
	view := m.View()
	for _, ref := range []string{"origin/main", "origin/release-perf"} {
		if !strings.Contains(view, ref) {
			t.Errorf("view is missing %q:\n%s", ref, view)
		}
	}
}

func lineContaining(t *testing.T, view, want string) string {
	t.Helper()
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, want) {
			return l
		}
	}
	t.Fatalf("no line containing %q in:\n%s", want, view)
	return ""
}
