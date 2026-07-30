package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Sknoww/drift/internal/git"
	"github.com/Sknoww/drift/internal/store"
)

// The ref that caused the v0.3.0 incident, in shape: a colleague's live ticket
// branch whose last path segment happens to be a main's name. deriveKey seeded
// the key `mvp-3` from it, the wizard's recency sort put it above the real
// `origin/mvp-3`, and every screen after that showed only the key — which was
// exactly the string the user was looking for.
const (
	incidentKey = "mvp-3"
	incidentRef = "origin/fix/PSOT-22114-PickHistory-API-for-audit/mvp-3"
)

func targetsModel(cfg store.Config, paths store.Paths) Model {
	m := New(git.New(t_nowhere), paths, cfg, store.Store{}, store.Prefs{})
	m.loading = false
	m.screen = screenTargets
	m.width, m.height = 100, 24
	return m
}

// --- getting there and back ----------------------------------------------

func TestDashboardOpensAndClosesTargets(t *testing.T) {
	m := newModel()

	next, _ := m.dispatch(ActionTargets)
	m = next.(Model)
	if m.screen != screenTargets {
		t.Fatalf("ActionTargets left screen = %v, want screenTargets", m.screen)
	}
	if m.targetsCur != 0 {
		t.Errorf("opened with cursor %d, want 0", m.targetsCur)
	}

	next, _ = m.dispatch(ActionCancel)
	if got := next.(Model).screen; got != screenDashboard {
		t.Errorf("esc on targets left screen = %v, want screenDashboard", got)
	}
}

func TestTargetsKeyReachesTheScreen(t *testing.T) {
	if got, ok := DefaultDashboardKeys().action("t"); !ok || got != ActionTargets {
		t.Errorf("dashboard t -> %q, %v; want %q", got, ok, ActionTargets)
	}
	// The same letter on the pairing checklist opens the *picker*, and the two
	// must stay separately bound: one shows the configured targets, the other
	// assigns one. A shared action would have to describe itself as both.
	if got, ok := DefaultPairingKeys().action("t"); !ok || got != ActionOpenPicker {
		t.Errorf("pairing t -> %q, %v; want %q", got, ok, ActionOpenPicker)
	}
	if actionText[ActionTargets] == actionText[ActionOpenPicker] {
		t.Errorf("ActionTargets and ActionOpenPicker describe themselves identically: %q", actionText[ActionTargets])
	}
}

func TestTargetsCursorClamps(t *testing.T) {
	m := targetsModel(sampleConfig(), samplePaths()) // two targets

	next, _ := m.dispatch(ActionMoveUp)
	if got := next.(Model).targetsCur; got != 0 {
		t.Errorf("up at the top: cursor = %d, want 0", got)
	}
	for i := 0; i < 5; i++ {
		next, _ = next.(Model).dispatch(ActionMoveDown)
	}
	if got := next.(Model).targetsCur; got != 1 {
		t.Errorf("down past the end: cursor = %d, want 1", got)
	}
}

// --- the point of the screen ---------------------------------------------

// The whole of 19e in one assertion: the ref is on screen, not just the key.
// Every screen the user lives in shows Target.Key, which is why a target
// pointing at a colleague's ticket branch looked correct right up to the moment
// `u` published a merge of it.
func TestTargetsShowsTheRefAndNotOnlyTheKey(t *testing.T) {
	cfg := store.Config{Targets: []store.Target{
		{Key: incidentKey, Ref: incidentRef},
		{Key: "r2stab", Ref: "origin/release-2-stability"},
	}}
	view := targetsModel(cfg, samplePaths()).View()

	for _, want := range []string{incidentKey, "r2stab", "origin/release-2-stability"} {
		if !strings.Contains(view, want) {
			t.Errorf("%q is not on screen; got:\n%s", want, view)
		}
	}
	// The head of the wrong ref is what gives it away. Asserted as the head
	// rather than the whole string so this test is about what the reader sees
	// first, not about whether the column happened to fit.
	if !strings.Contains(view, "origin/fix/PSOT-22114") {
		t.Errorf("the ref's head is not on screen; got:\n%s", view)
	}
}

// A ref too long for its column ellipsises at the *tail*. That is the load-
// bearing end: `origin/fix/PSOT-22114-…` is the half that says "this is
// somebody's ticket branch", and the trailing `/mvp-3` is the half that made it
// look like a main. A middle-elide would show `origin/fix/…/mvp-3` and hide the
// one thing worth reading.
func TestTargetsRefKeepsItsHeadWhenItOverflows(t *testing.T) {
	cfg := store.Config{Targets: []store.Target{{Key: incidentKey, Ref: incidentRef}}}

	m := targetsModel(cfg, samplePaths())
	m.width = minTerminalWidth // the narrowest terminal drift will draw into
	view := m.View()

	if !strings.Contains(view, "origin/fix/PSOT") {
		t.Errorf("the ref's head was lost at %d columns; got:\n%s", m.width, view)
	}
	if strings.Contains(view, incidentRef) {
		t.Fatalf("the ref fit whole at %d columns — this test is no longer measuring truncation", m.width)
	}
	if !strings.Contains(view, "…") {
		t.Errorf("a truncated ref is not marked as truncated; got:\n%s", view)
	}
}

// `e` re-points a ref, and nothing else in the file — so the screen still names
// config.json. A user who reads `e` and assumes it reaches the key, the
// unmergeable classes or the declare allow-list would be wrong, and the path is
// how they find where those actually live.
func TestTargetsNamesTheConfigFile(t *testing.T) {
	paths := samplePaths()
	if view := targetsModel(sampleConfig(), paths).View(); !strings.Contains(view, paths.Config) {
		t.Errorf("the config path is not on screen; got:\n%s", view)
	}
}

func TestTargetsEmptyConfigSaysSo(t *testing.T) {
	view := targetsModel(store.Config{}, samplePaths()).View()
	if !strings.Contains(view, "No targets configured") {
		t.Errorf("an empty target list rendered no explanation; got:\n%s", view)
	}
}

// --- the frame ------------------------------------------------------------

// The area-14 invariant, on the newest list screen: the frame stays inside the
// terminal and the cursor's row is always drawn. Run at the width floor with a
// config path long enough to wrap its header line, which is the exact failure
// area 15 found in the wizard — prose breaking the row budget rather than rows.
func TestTargetsBoundsTheFrame(t *testing.T) {
	const height = 24

	targets := make([]store.Target, 60)
	for i := range targets {
		targets[i] = store.Target{
			Key: fmt.Sprintf("t%02d", i),
			Ref: fmt.Sprintf("origin/release/%02d-a-fairly-long-branch-name", i),
		}
	}
	paths := samplePaths()
	paths.Config = "/Users/someone/dev/repos/a-rather-deeply-nested-work-repo/.git/drift/config.json"

	for _, width := range []int{minTerminalWidth, 80, 100} {
		m := targetsModel(store.Config{Targets: targets}, paths)
		m.width, m.height = width, height

		for _, cursor := range []int{0, 30, len(targets) - 1} {
			m.targetsCur = cursor
			view := m.View()
			if lines := strings.Count(view, "\n") + 1; lines > height {
				t.Fatalf("width %d cursor %d: frame is %d lines on a %d-line terminal:\n%s",
					width, cursor, lines, height, view)
			}
			if !strings.Contains(view, targets[cursor].Key) {
				t.Fatalf("width %d cursor %d: the selected target is not on screen:\n%s", width, cursor, view)
			}
		}
	}
}

// --- the ? overlay --------------------------------------------------------

func TestTargetsHelpOverlayIsAboutThisScreen(t *testing.T) {
	m := targetsModel(sampleConfig(), samplePaths())
	m.showHelp = true
	view := m.View()

	if !strings.Contains(view, "Keys — targets") {
		t.Errorf("the overlay does not name the screen it was opened from; got:\n%s", view)
	}
	// This screen draws no glyphs, so it gets no legend — an empty "Glyphs"
	// heading would promise an explanation with nothing under it, and the
	// dashboard's legend would explain signals that are not on this screen.
	if strings.Contains(view, "Glyphs") {
		t.Errorf("the overlay drew a glyph section for a screen with no glyphs; got:\n%s", view)
	}
	if !strings.Contains(view, actionText[ActionCancel]) {
		t.Errorf("the overlay does not say how to leave; got:\n%s", view)
	}
}

// The screens that do have glyphs must keep them — the guard above is a
// carve-out for one screen, not a regression in the section itself.
func TestHelpOverlayKeepsGlyphsWhereThereAreSome(t *testing.T) {
	m := newModel()
	m.width, m.height = 100, 24
	m.showHelp = true
	if view := m.View(); !strings.Contains(view, "Glyphs") {
		t.Errorf("the dashboard overlay lost its glyph legend; got:\n%s", view)
	}
}

func TestDashboardOverlayAdvertisesTargets(t *testing.T) {
	m := newModel()
	m.width, m.height = 100, 40 // tall enough that nothing scrolls out of reach
	m.showHelp = true
	if view := m.View(); !strings.Contains(view, actionText[ActionTargets]) {
		t.Errorf("the dashboard overlay does not mention the targets screen; got:\n%s", view)
	}
}

// --- re-pointing a target (the editing half of 19e) -----------------------

// A fixed clock, so the picker's age column is assertable. The refs below are
// dated against it.
var repointNow = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func sampleRefs() []git.RemoteBranch {
	// Recency order, as git.RemoteBranches returns them — and in the shape that
	// caused the incident: the busy ticket branch on top, the real main below it.
	return []git.RemoteBranch{
		{Ref: incidentRef, Updated: repointNow.Add(-2 * time.Hour)},
		{Ref: "origin/mvp-3", Updated: repointNow.Add(-48 * time.Hour)},
		{Ref: "origin/release-2-stability", Updated: repointNow.Add(-30 * 24 * time.Hour)},
	}
}

// repointModel opens the ref picker over a target and hands it git's answer,
// without ever dialing git.
func repointModel(cfg store.Config, refs []git.RemoteBranch) Model {
	m := targetsModel(cfg, samplePaths())
	next, _ := m.dispatch(ActionRepoint) // the load Cmd it returns is never run
	m = next.(Model)
	m.repoint.now = repointNow
	return m.applyRemoteRefs(remoteRefsMsg{refs: refs})
}

func incidentConfig() store.Config {
	return store.Config{Targets: []store.Target{
		{Key: incidentKey, Ref: incidentRef},
		{Key: "r2stab", Ref: "origin/release-2-stability"},
	}}
}

func TestRepointKeyOpensThePickerForTheSelectedTarget(t *testing.T) {
	if got, ok := DefaultTargetsKeys().action("e"); !ok || got != ActionRepoint {
		t.Errorf("targets e -> %q, %v; want %q", got, ok, ActionRepoint)
	}
	// The wizard's `e` renames a key; this one changes a ref. Two verbs, so the
	// generated help table can describe each on its own.
	if actionText[ActionRepoint] == actionText[ActionEditKey] {
		t.Errorf("ActionRepoint and ActionEditKey describe themselves identically: %q", actionText[ActionRepoint])
	}
	// enter stays unbound on the list: it means "commit this screen" everywhere
	// else, and there is nothing here to commit until `e` opens something.
	if got, ok := DefaultTargetsKeys().action("enter"); ok {
		t.Errorf("targets enter -> %q, want it unbound", got)
	}

	m := repointModel(incidentConfig(), sampleRefs())
	if !m.repoint.open {
		t.Fatal("e did not open the picker")
	}
	if m.repoint.key != incidentKey || m.repoint.from != incidentRef {
		t.Errorf("picker opened for %q/%q, want %q/%q",
			m.repoint.key, m.repoint.from, incidentKey, incidentRef)
	}
}

// A target list with nothing in it has nothing to re-point, and `e` must refuse
// rather than act on target 0 — the rule every cursor-driven verb here follows.
func TestRepointRefusesWithNoTargets(t *testing.T) {
	m := targetsModel(store.Config{}, samplePaths())
	next, cmd := m.dispatch(ActionRepoint)
	if next.(Model).repoint.open {
		t.Error("e opened the picker with no target selected")
	}
	if cmd != nil {
		t.Error("e asked git for refs with no target to re-point")
	}
}

// The picker offers real refs in git's order, and marks the one the target
// already names. Without that mark the list is refs with nothing to distinguish
// them, and the user cannot see what they are changing *from*.
func TestRepointPickerShowsTheRefsAndMarksTheCurrentOne(t *testing.T) {
	view := repointModel(incidentConfig(), sampleRefs()).View()

	for _, want := range []string{"origin/mvp-3", "origin/release-2-stability", "origin/fix/PSOT-22114"} {
		if !strings.Contains(view, want) {
			t.Errorf("%q is not on screen; got:\n%s", want, view)
		}
	}
	if !strings.Contains(view, currentRefLabel) {
		t.Errorf("the ref the target already points at is not marked; got:\n%s", view)
	}
	// The age column is what makes the recency order legible rather than arbitrary
	// — the wizard's argument, and this is the wizard's list again.
	if !strings.Contains(view, "2d") {
		t.Errorf("the age column is missing; got:\n%s", view)
	}
	if !strings.Contains(view, incidentKey) {
		t.Errorf("the picker does not say which target it is for; got:\n%s", view)
	}
}

func TestRepointPickerSaysWhenThereAreNoRefs(t *testing.T) {
	view := repointModel(incidentConfig(), nil).View()
	if !strings.Contains(view, "No remote-tracking refs") {
		t.Errorf("an empty ref list rendered no explanation; got:\n%s", view)
	}
}

// Picking a ref opens the confirmation and writes nothing. The read-only-until-
// the-last-moment ordering `s` and `u` are built on: a flow that backs out here
// has touched nothing and has nothing to undo.
func TestRepointPickDoesNotWriteYet(t *testing.T) {
	m := repointModel(incidentConfig(), sampleRefs())
	next, _ := m.dispatch(ActionMoveDown) // onto origin/mvp-3
	next, cmd := next.(Model).dispatch(ActionConfirm)
	m = next.(Model)

	if !m.repoint.confirm {
		t.Fatal("enter on a ref did not open the confirmation")
	}
	if cmd != nil {
		t.Error("enter on a ref fired a Cmd before the confirmation was answered")
	}
	if got, _ := m.cfg.Target(incidentKey); got.Ref != incidentRef {
		t.Errorf("the config changed before confirmation: ref = %q", got.Ref)
	}
	if m.repoint.to != "origin/mvp-3" {
		t.Errorf("pending ref = %q, want origin/mvp-3", m.repoint.to)
	}
}

// The confirmation names both refs, and the *from* matters as much as the *to*:
// the whole finding behind 19e is that the ref being replaced is the one the user
// has never seen, because its key read correctly.
func TestRepointConfirmNamesBothRefs(t *testing.T) {
	m := repointModel(incidentConfig(), sampleRefs())
	next, _ := m.dispatch(ActionMoveDown)
	next, _ = next.(Model).dispatch(ActionConfirm)
	view := next.(Model).View()

	for _, want := range []string{incidentKey, "origin/fix/PSOT-22114", "origin/mvp-3", "(y/n)"} {
		if !strings.Contains(view, want) {
			t.Errorf("%q is not on the confirmation; got:\n%s", want, view)
		}
	}
}

// A long ref on the confirmation ellipsises at its **tail**, the end 19a settled:
// `origin/fix/PSOT-22114-…` is what gives a wrong target away, and the trailing
// `/mvp-3` is the half that made it look right. A middle-elide showing
// `origin/fix/…/mvp-3` is the obvious later "improvement" and would hide the one
// thing worth reading.
func TestRepointConfirmKeepsTheRefsHead(t *testing.T) {
	m := repointModel(incidentConfig(), sampleRefs())
	m.width = minTerminalWidth
	next, _ := m.dispatch(ActionMoveDown)
	next, _ = next.(Model).dispatch(ActionConfirm)
	view := next.(Model).View()

	if strings.Contains(view, incidentRef) {
		t.Fatalf("the ref fit whole at %d columns — this test is no longer measuring truncation:\n%s",
			minTerminalWidth, view)
	}
	if !strings.Contains(view, "origin/fix/PSOT") {
		t.Errorf("the ref's head was lost at %d columns; got:\n%s", minTerminalWidth, view)
	}
	if !strings.Contains(view, "…") {
		t.Errorf("a truncated ref is not marked as truncated; got:\n%s", view)
	}
	// The prose has to survive the floor whole: it carries what the write does,
	// and a sentence clipped mid-word is the failure 17b measured in its own
	// overlay at 80 columns.
	if !strings.Contains(view, "config.json.") {
		t.Errorf("the confirmation's prose was clipped at %d columns; got:\n%s", minTerminalWidth, view)
	}
}

// Declining costs nothing, because nothing has happened yet.
func TestRepointDeclineChangesNothing(t *testing.T) {
	m := repointModel(incidentConfig(), sampleRefs())
	next, _ := m.dispatch(ActionMoveDown)
	next, _ = next.(Model).dispatch(ActionConfirm)
	next, cmd := next.(Model).dispatch(ActionCancel)
	m = next.(Model)

	if m.repoint.open || m.repoint.confirm {
		t.Error("n left the re-point flow open")
	}
	if cmd != nil {
		t.Error("n fired a Cmd")
	}
	if got, _ := m.cfg.Target(incidentKey); got.Ref != incidentRef {
		t.Errorf("n changed the config: ref = %q", got.Ref)
	}
	if m.screen != screenTargets {
		t.Errorf("n left screen = %v, want to stay on the targets list", m.screen)
	}
}

// Confirming writes the config with exactly one ref changed. The Cmd is run here
// rather than mocked: the repo it is built over is not a repository, so the save
// fails — but the config it carries is what a real save would have written, and
// that is the assertion.
func TestRepointConfirmWritesTheNewRef(t *testing.T) {
	m := repointModel(incidentConfig(), sampleRefs())
	next, _ := m.dispatch(ActionMoveDown)
	next, _ = next.(Model).dispatch(ActionConfirm)
	next, cmd := next.(Model).dispatch(ActionConfirm) // y
	m = next.(Model)

	if m.repoint.open || m.repoint.confirm {
		t.Error("y left the re-point flow open")
	}
	if cmd == nil {
		t.Fatal("y fired no Cmd")
	}
	msg, ok := cmd().(repointMsg)
	if !ok {
		t.Fatalf("y's Cmd produced %T, want repointMsg", cmd())
	}
	if msg.key != incidentKey || msg.from != incidentRef || msg.to != "origin/mvp-3" {
		t.Errorf("repointMsg = %q %q -> %q, want %q %q -> origin/mvp-3",
			msg.key, msg.from, msg.to, incidentKey, incidentRef)
	}
	if got, _ := msg.cfg.Target(incidentKey); got.Ref != "origin/mvp-3" {
		t.Errorf("the config it would write has ref = %q, want origin/mvp-3", got.Ref)
	}
	if got, _ := msg.cfg.Target("r2stab"); got.Ref != "origin/release-2-stability" {
		t.Errorf("the other target changed too: ref = %q", got.Ref)
	}
}

// The rule areas 5 and 6 both landed on, and this is its widest case: a
// re-pointed target changes *every* paired branch's ↓behind at once, so the
// sweep has to run again rather than be assumed still true.
func TestRepointResweepsAfterTheWrite(t *testing.T) {
	m := repointModel(incidentConfig(), sampleRefs())
	cfg, _ := m.cfg.SetTargetRef(incidentKey, "origin/mvp-3")

	before := m.sweepID
	next, cmd := m.applyRepoint(repointMsg{cfg: cfg, key: incidentKey, from: incidentRef, to: "origin/mvp-3"})

	if got, _ := next.cfg.Target(incidentKey); got.Ref != "origin/mvp-3" {
		t.Errorf("the new config was not folded in: ref = %q", got.Ref)
	}
	if !next.loading {
		t.Error("no sweep was started after the write")
	}
	if next.sweepID == before {
		t.Error("the sweep id did not advance, so an in-flight sweep could clobber the new one")
	}
	if cmd == nil {
		t.Fatal("applyRepoint fired no Cmd")
	}
}

// A failed write leaves the model alone. Folding the config in optimistically
// would leave every ↓behind measured against a ref config.json does not hold.
func TestRepointFailureKeepsTheOldConfig(t *testing.T) {
	m := repointModel(incidentConfig(), sampleRefs())
	cfg, _ := m.cfg.SetTargetRef(incidentKey, "origin/mvp-3")

	next, cmd := m.applyRepoint(repointMsg{
		cfg: cfg, key: incidentKey, to: "origin/mvp-3", err: errors.New("read-only file system")})

	if got, _ := next.cfg.Target(incidentKey); got.Ref != incidentRef {
		t.Errorf("a failed write still changed the config: ref = %q", got.Ref)
	}
	if cmd != nil {
		t.Error("a failed write started a sweep")
	}
	if !strings.Contains(next.notice, incidentKey) || !strings.Contains(next.notice, "read-only file system") {
		t.Errorf("notice = %q, want it to name the target and the error", next.notice)
	}
}

// Picking the ref already configured is said out loud rather than written. It is
// the one outcome where "it worked" and "nothing happened" leave the row looking
// identical, so the screen has to be the thing that tells them apart.
func TestRepointToTheSameRefIsANoOp(t *testing.T) {
	m := repointModel(incidentConfig(), sampleRefs())
	next, cmd := m.dispatch(ActionConfirm) // cursor 0 is the ref it already names
	m = next.(Model)

	if m.repoint.confirm {
		t.Error("picking the current ref opened a confirmation for a change of nothing")
	}
	if cmd != nil {
		t.Error("picking the current ref fired a Cmd")
	}
	if !strings.Contains(m.notice, "already points at") {
		t.Errorf("notice = %q, want it to say nothing changed", m.notice)
	}
}

// --- the picker's filter --------------------------------------------------

// This is the wizard's list again — every ref in the repo — so it is the one
// place area 14 found filtering load-bearing. And a ref is exactly the kind of
// string that contains the screen's own verbs, so while the field has focus `e`
// and `j` must type rather than act.
func TestRepointFilterNarrowsAndSwallowsTheVerbs(t *testing.T) {
	m := repointModel(incidentConfig(), sampleRefs())

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = next.(Model)
	if !m.repoint.filter.open {
		t.Fatal("/ did not open the filter field")
	}
	for _, r := range "release" {
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
	}
	if got := m.repoint.filter.query(); got != "release" {
		t.Fatalf("query = %q, want %q — a verb key acted instead of typing", got, "release")
	}
	if got := len(m.repoint.visible()); got != 1 {
		t.Errorf("%d refs match \"release\", want 1", got)
	}

	view := m.View()
	if !strings.Contains(view, "1 of 3") {
		t.Errorf("the match counts are not on screen; got:\n%s", view)
	}

	// `e` is the screen's own verb underneath, and must be typeable here.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = next.(Model)
	if got := m.repoint.filter.query(); got != "releasee" {
		t.Errorf("query = %q, want e to have typed", got)
	}
	if m.repoint.confirm {
		t.Error("typing into the filter opened the confirmation")
	}
}

// esc means one thing at a time: with a query applied it clears the query, and
// only then does it close the picker. The wizard's rule, and it exists there
// because the version without it quit first-run setup outright.
func TestRepointEscClearsTheFilterBeforeTheScreen(t *testing.T) {
	m := repointModel(incidentConfig(), sampleRefs())

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyEnter}) // accept the query
	m = next.(Model)
	if !m.repoint.filter.active() || m.repoint.filter.open {
		t.Fatalf("enter should keep the query and close the field; open=%v active=%v",
			m.repoint.filter.open, m.repoint.filter.active())
	}

	next, _ = m.dispatch(ActionCancel)
	m = next.(Model)
	if m.repoint.filter.active() {
		t.Error("esc did not clear the filter")
	}
	if !m.repoint.open {
		t.Error("esc closed the picker instead of clearing the filter")
	}

	next, _ = m.dispatch(ActionCancel)
	if next.(Model).repoint.open {
		t.Error("a second esc did not close the picker")
	}
	if next.(Model).screen != screenTargets {
		t.Error("esc out of the picker left the targets screen too")
	}
}

// --- the confirmation's help overlay --------------------------------------

// The one screen in this flow where the key that opens the help would otherwise
// commit the write. "Any key closes, and is consumed" is what makes ? safe here,
// exactly as it is on the stash plan (17b).
func TestRepointConfirmHelpKeyDoesNotWrite(t *testing.T) {
	m := repointModel(incidentConfig(), sampleRefs())
	next, _ := m.dispatch(ActionMoveDown)
	next, _ = next.(Model).dispatch(ActionConfirm)
	m = next.(Model)

	next, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = next.(Model)
	if !m.showHelp {
		t.Fatal("? did not open the help over the confirmation")
	}
	if cmd != nil {
		t.Error("? fired a Cmd")
	}
	if !m.repoint.confirm {
		t.Error("? answered the confirmation")
	}
	if !strings.Contains(m.View(), "re-point confirmation") {
		t.Errorf("the overlay does not name the confirmation; got:\n%s", m.View())
	}

	// Closing it is consumed: the key that dismisses the help must not also
	// answer the question underneath.
	next, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = next.(Model)
	if m.showHelp {
		t.Error("y did not close the overlay")
	}
	if cmd != nil || !m.repoint.confirm {
		t.Error("the key that closed the overlay also committed the write")
	}
}

// --- the frame ------------------------------------------------------------

// Area 14's invariant on the newest list: the frame stays inside the terminal and
// the cursor's row is always drawn, at every width down to the floor. The refs are
// long and numerous, which is the repo shape the whole area exists for.
func TestRepointPickerBoundsTheFrame(t *testing.T) {
	const height = 24

	refs := make([]git.RemoteBranch, 400)
	for i := range refs {
		refs[i] = git.RemoteBranch{
			Ref:     fmt.Sprintf("origin/fix/TEAM-%04d-a-fairly-long-branch-name/mvp-3", i),
			Updated: repointNow.Add(-time.Duration(i) * time.Hour),
		}
	}

	for _, width := range []int{minTerminalWidth, 80, 100} {
		m := repointModel(incidentConfig(), refs)
		m.width, m.height = width, height

		for _, cursor := range []int{0, 200, len(refs) - 1} {
			m.repoint.cursor = cursor
			view := m.View()
			if lines := strings.Count(view, "\n") + 1; lines > height {
				t.Fatalf("width %d cursor %d: frame is %d lines on a %d-line terminal:\n%s",
					width, cursor, lines, height, view)
			}
			// The selected row is on screen. Asserted on the ref's head, which is the
			// half the column keeps.
			head := fmt.Sprintf("origin/fix/TEAM-%04d", cursor)
			if !strings.Contains(view, head) {
				t.Fatalf("width %d cursor %d: the selected ref is not on screen:\n%s", width, cursor, view)
			}
		}
	}
}

// --- 19e end to end, against a real repo ----------------------------------

// The whole editing half in one run, against a real repository and a real
// config.json: the picker offers the repo's own refs, the confirmation names both
// of them, and the file on disk comes back with one field changed. Driven through
// the model rather than around it, because the wiring — the Cmd, store.SaveConfig
// resolving the git dir for itself, and the re-sweep — is what a unit test with a
// hand-built config cannot check.
func TestRepointEndToEndRewritesTheConfig(t *testing.T) {
	origin := repointOrigin(t)
	dir := t.TempDir()
	rungit(t, dir, "clone", "--quiet", origin, dir)

	repo := git.New(dir)
	paths, err := store.Resolve(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	// The state the incident left a user in: one target keyed correctly and
	// pointing at somebody's ticket branch.
	wrong := "origin/fix/PSOT-22114-PickHistory/mvp-3"
	cfg := store.Config{Targets: []store.Target{
		{Key: incidentKey, Ref: wrong},
		{Key: "r2stab", Ref: "origin/release-2-stability"},
	}}
	if err := store.SaveConfig(context.Background(), repo, cfg); err != nil {
		t.Fatal(err)
	}

	m := New(repo, paths, cfg, store.Store{}, store.Prefs{})
	m.loading = false
	m.width, m.height = 100, 24
	next, _ := m.dispatch(ActionTargets)
	m = next.(Model)

	// The refs come from git, not from a fixture.
	next, cmd := m.dispatch(ActionRepoint)
	m = next.(Model)
	if cmd == nil {
		t.Fatal("e asked git for nothing")
	}
	refs, ok := cmd().(remoteRefsMsg)
	if !ok {
		t.Fatalf("e's Cmd produced %T, want remoteRefsMsg", cmd())
	}
	if refs.err != nil {
		t.Fatal(refs.err)
	}
	m = m.applyRemoteRefs(refs)

	// Walk onto origin/mvp-3 the way a user would, rather than setting the cursor.
	target := "origin/mvp-3"
	found := false
	for i := 0; i < len(m.repoint.refs); i++ {
		if ref, ok := m.repoint.selectedRef(); ok && ref == target {
			found = true
			break
		}
		next, _ = m.dispatch(ActionMoveDown)
		m = next.(Model)
	}
	if !found {
		t.Fatalf("%s is not on offer; the picker has %+v", target, m.repoint.refs)
	}

	next, _ = m.dispatch(ActionConfirm) // pick it
	m = next.(Model)
	view := m.View()
	if !strings.Contains(view, wrong) || !strings.Contains(view, target) {
		t.Errorf("the confirmation does not name both refs; got:\n%s", view)
	}

	next, cmd = m.dispatch(ActionConfirm) // y
	m = next.(Model)
	if cmd == nil {
		t.Fatal("y fired no Cmd")
	}
	msg, ok := cmd().(repointMsg)
	if !ok {
		t.Fatalf("y's Cmd produced %T, want repointMsg", cmd())
	}
	if msg.err != nil {
		t.Fatalf("the write failed: %v", msg.err)
	}
	m, _ = m.applyRepoint(msg)

	// The file on disk is the assertion — not the model, which is the thing that
	// could be lying.
	saved, _, err := store.LoadConfig(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := saved.Target(incidentKey); got.Ref != target {
		t.Errorf("config.json has %s -> %q, want %q", incidentKey, got.Ref, target)
	}
	if got, _ := saved.Target("r2stab"); got.Ref != "origin/release-2-stability" {
		t.Errorf("the other target was rewritten too: %q", got.Ref)
	}

	// And the screen the user is left on shows the corrected ref, which is the
	// durable feedback the flow deliberately relies on instead of a notice.
	if !strings.Contains(m.View(), target) {
		t.Errorf("the targets list still shows the old ref; got:\n%s", m.View())
	}
}

// repointOrigin is a bare repo carrying the three refs the picker should offer:
// the real main, a long-lived branch, and the ticket branch whose tail reads like
// a main's name.
func repointOrigin(t *testing.T) string {
	t.Helper()
	work := newTestRepo(t)
	rungit(t, work, "branch", "mvp-3")
	rungit(t, work, "branch", "release-2-stability")
	rungit(t, work, "branch", "fix/PSOT-22114-PickHistory/mvp-3")

	bare := t.TempDir()
	rungit(t, work, "init", "--quiet", "--bare", "--initial-branch=main", bare)
	rungit(t, work, "push", "--quiet", bare,
		"main", "mvp-3", "release-2-stability", "fix/PSOT-22114-PickHistory/mvp-3")
	return bare
}
