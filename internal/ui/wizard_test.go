package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Sknoww/drift/internal/git"
	"github.com/Sknoww/drift/internal/store"
)

// The wizard's git (listing remote refs) and IO (writing config) live in the
// caller; these tests drive the selection logic that decides what gets written.

func wizardWith(refs ...string) wizardModel {
	return newWizard(remoteBranches(refs...), store.Prefs{})
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

// A key is a terse UI label. deriveKey keeps the whole path after the remote, and
// where it cannot keep the whole path it seeds *nothing* rather than inventing a
// shorter name for it (area 19d). Depth decides, then width: a single segment is
// the ref's own name at any length, while a deep path is seeded only while it
// stays terse.
func TestDeriveKeySeedsATerseKey(t *testing.T) {
	cases := map[string]string{
		"origin/main":         "main",
		"origin/release-perf": "release-perf",
		"upstream/main":       "main",

		// Short enough to keep whole: release/2.0 says what hotfix/2.0 does not.
		"origin/feature/x":   "feature/x",
		"origin/release/2.0": "release/2.0",

		// A deep path past the threshold. The old rule cut to the last segment
		// here; that is what let a ticket branch inherit a main's name (see the
		// test below).
		"origin/feature/TEAM-1234-some-long-name": "",

		// One segment, so the threshold has no say: long, but it is the branch's
		// name and nothing was dropped to reach it. The column bounds it and e
		// renames it. This is a real main's shape in the repo area 19 came from.
		"origin/release-2-stability":               "release-2-stability",
		"origin/a-very-long-single-segment-branch": "a-very-long-single-segment-branch",

		// No remote prefix at all: nothing to strip, and the whole ref is the path.
		"main": "main",
	}
	for ref, want := range cases {
		if got := deriveKey(ref); got != want {
			t.Errorf("deriveKey(%q) = %q, want %q", ref, got, want)
		}
	}
}

// The invariant area 19d bought, and the one that makes the seed unable to lie:
// **a seeded key is the ref's whole path after the remote, or nothing.** Put a
// seeded key back under its ref's remote and you get the ref itself.
//
// Pinned as a property rather than as a table of expectations because the failure
// it guards against is a *new* shortening rule being added later — an initialism,
// a middle-elide, a last-segment fallback reintroduced under another name. Each
// would pass a table that was updated alongside it; none can pass this.
func TestASeededKeyAlwaysReconstructsItsRef(t *testing.T) {
	refs := []string{
		"origin/main",
		"origin/mvp-3",
		"origin/release/2.0",
		"origin/hotfix/2.0",
		"origin/feature/TEAM-1234-some-long-name",
		"origin/fix/PSOT-22114-PickHistory-API-for-audit/mvp-3",
		"origin/releases/2024/lts-maintenance",
		"origin/release-2-stability",
		"origin/a-very-long-single-segment-branch",
		"upstream/main",
		"main",
	}
	for _, ref := range refs {
		key := deriveKey(ref)
		if key == "" {
			continue // no seed offered: there is no name here that could be wrong
		}
		rebuilt := key
		if remote, _, found := strings.Cut(ref, "/"); found {
			rebuilt = remote + "/" + key
		}
		if rebuilt != ref {
			t.Errorf("deriveKey(%q) = %q, which names %q — a key must reconstruct its own ref",
				ref, key, rebuilt)
		}
	}
}

// The area-19 incident, at its source. A colleague's ticket branch ending in
// /mvp-3 sorted above the real origin/mvp-3 (recency, area 14) and the old
// last-segment fallback labelled it `mvp-3` — the exact string the user was
// looking for. Selecting it recorded a target whose key read correctly on every
// dashboard row and whose ref pointed at a feature branch, and `u` published a
// merge into an open merge request.
//
// Now the deep ref is offered with no name at all: its row says so, selecting it
// blocks the save, and the block names the *ref* — so a target called mvp-3 can
// only exist if the user typed mvp-3 while looking at what it points at.
func TestWizardWillNotSeedAKeyThatLiesAboutItsRef(t *testing.T) {
	const lie = "origin/fix/PSOT-22114-PickHistory-API-for-audit/mvp-3"
	m := wizardWith(lie, "origin/mvp-3")
	m.width, m.height = 100, 24

	if got := m.targets[0].key; got != "" {
		t.Fatalf("key seeded for %q = %q, want none — it is not that branch's name", lie, got)
	}
	if got := m.targets[1].key; got != "mvp-3" {
		t.Errorf("key for origin/mvp-3 = %q, want mvp-3 — the honest ref must still seed", got)
	}

	// The row states what it lacks and how to supply it, rather than showing a
	// blank cell that reads as a rendering fault.
	if view := m.View(); !strings.Contains(view, keyPrompt) {
		t.Errorf("an unseeded row must prompt for a name:\n%s", view)
	}

	// Selecting it and saving is refused, with the ref named — never the key,
	// which is the string that made the wrong target look right (19a's rule).
	m.targets[0].included = true
	out, cmd := m.dispatch(ActionConfirm)
	final := out.(wizardModel)
	if final.done || cmd != nil {
		t.Fatal("saving a selected ref with no key must be blocked")
	}
	if !strings.Contains(final.notice, lie) {
		t.Errorf("notice = %q, want it to name the ref it is asking about", final.notice)
	}

	// And it is nameable: e, type it, save. The user's own choice, made against
	// the ref on screen.
	out, _ = final.dispatch(ActionEditKey)
	edit := out.(wizardModel)
	edit.input.SetValue("psot-22114")
	out, _ = edit.commitEdit().dispatch(ActionConfirm)
	saved := out.(wizardModel)
	if len(saved.result) != 1 || saved.result[0].Key != "psot-22114" || saved.result[0].Ref != lie {
		t.Errorf("saved = %+v, want the hand-typed key against the picked ref", saved.result)
	}
}

// An unseeded row is quiet until it is selected, and then it is not. A repo of
// deep-pathed feature branches must not open first-run setup on a screenful of
// alarms about rows the user has never touched — the pairing checklist's own
// grammar, where ⚠ pick a target appears only on an included candidate.
func TestWizardPromptsQuietlyUntilTheRowIsSelected(t *testing.T) {
	m := wizardWith("origin/feature/TEAM-1234-some-long-name")
	m.width, m.height = 100, 24

	text, style := m.keyCell(m.targets[0])
	if text != keyPrompt {
		t.Errorf("unselected prompt = %q, want %q with no warning glyph", text, keyPrompt)
	}
	// Compared by the style's own foreground rather than by its rendered output: a
	// test's color profile emits no color at all, so Render would make every style
	// on the screen look identical (DESIGN.md §3 — the class of bug that hid area
	// 3's two band traps from the suite).
	if style.GetForeground() != m.styles.help.GetForeground() {
		t.Error("an untouched row is missing nothing yet — the prompt must not be styled as an error")
	}

	m.targets[0].included = true
	text, style = m.keyCell(m.targets[0])
	if !strings.HasPrefix(text, "⚠ ") {
		t.Errorf("selected prompt = %q, want it flagged — it now blocks the save", text)
	}
	if style.GetForeground() != m.styles.errText.GetForeground() {
		t.Error("a selection that blocks the save must be styled as the blocker it is")
	}
}

// The prompt is drawn *in* the key column, so the column has to be sized to what
// it draws rather than to the empty key behind it — otherwise the prompt
// overflows and every ← on the screen goes out of line.
func TestWizardKeyColumnFitsThePrompt(t *testing.T) {
	m := wizardWith("origin/feature/TEAM-1234-some-long-name", "origin/main")
	m.width, m.height = 100, 24
	m.targets[0].included = true

	promptText, _ := m.keyCell(m.targets[0])
	if got := m.keyColWidth(m.visible()); got < lipgloss.Width(promptText) {
		t.Errorf("key column = %d, too narrow for the prompt %q (%d cells)",
			got, promptText, lipgloss.Width(promptText))
	}

	// Every drawn arrow lands in the same column. Measured in display cells, not
	// bytes: the prompt carries a multi-byte ⚠ and the key beside it is ASCII, so
	// byte offsets would differ on two rows that line up perfectly on screen.
	var cols []int
	for _, line := range strings.Split(m.View(), "\n") {
		if i := strings.Index(line, "←"); i >= 0 {
			cols = append(cols, lipgloss.Width(line[:i]))
		}
	}
	if len(cols) != 2 {
		t.Fatalf("found %d rows with an arrow, want 2", len(cols))
	}
	if cols[0] != cols[1] {
		t.Errorf("arrows at columns %v — the key column did not align its rows", cols)
	}
}

// The wizard's key column is the one that padded every row past the panel width
// in area 14's measurements. deriveKey stopped seeding long keys; this stops a
// hand-typed one (e accepts 64 characters) doing the same.
//
// The panel is what bounds it, not a constant: area 20 removed the 24-cell cap,
// so what this asserts is the thing the cap was standing in for — the ref keeps
// a usable share whatever the key does.
func TestWizardKeyColumnIsBounded(t *testing.T) {
	m := wizardWith("origin/main", "origin/develop")
	m.width, m.height = 100, 24
	m.targets[0].key = strings.Repeat("x", 60)

	if got, avail := m.keyColWidth(m.visible()), rowWidth(m.styles, m.width)-wizardRowFixed-minNameCol; got > avail {
		t.Errorf("key column = %d, want at most %d — it left the ref nothing", got, avail)
	}

	view := m.View()
	for _, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > 100 {
			t.Fatalf("a %d-cell line on a 100-column terminal — the key column overflowed:\n%s", w, view)
		}
	}
	if !strings.Contains(view, "origin/develop") {
		t.Errorf("one long key pushed the other rows' refs off the panel:\n%s", view)
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
	}, store.Prefs{})
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
	}, store.Prefs{})
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
