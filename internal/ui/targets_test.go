package ui

import (
	"fmt"
	"strings"
	"testing"

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

// Editing a target is not built yet, so the screen names the file where a wrong
// one is corrected. That route — hand-editing config.json with drift closed —
// is what the v0.3.0 incident actually required of its user, and it is one
// nobody finds without reading the source.
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
