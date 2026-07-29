package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/Sknoww/drift/internal/git"
	"github.com/Sknoww/drift/internal/store"
)

// Theming (roadmap area 16b). Area 15 shipped four selection *shapes* with
// their colours baked in; these pin the split that lets a shape be rendered in
// someone else's accent — and, just as much, pin what deliberately stays out of
// the user's reach.

// The resolution order prefs.json established, now on a second setting:
// DRIFT_ACCENT is "for this run", the file is the saved decision, and Drift's
// own adaptive pair is what a machine with neither gets.
func TestAccentResolutionOrder(t *testing.T) {
	t.Setenv("DRIFT_ACCENT", "")
	if got, name := activeAccent(""); got != colAccent || name != "" {
		t.Errorf("nothing set resolved to %v (%q), want the adaptive default", got, name)
	}

	if got, name := activeAccent("208"); got != lipgloss.Color("208") || name != "208" {
		t.Errorf("a saved accent resolved to %v (%q), want 208", got, name)
	}

	t.Setenv("DRIFT_ACCENT", "#ff8800")
	if got, name := activeAccent("208"); got != lipgloss.Color("#ff8800") || name != "#ff8800" {
		t.Errorf("DRIFT_ACCENT lost to the preference: got %v (%q)", got, name)
	}

	// A shell typo falls through to the preference rather than to the default —
	// the override failed, so the saved choice is what is left. (A typo in
	// prefs.json cannot get this far: store.LoadPrefs refuses to start, because
	// an accent that quietly did not apply renders the default blue and looks
	// exactly like the requested one working.)
	t.Setenv("DRIFT_ACCENT", "tangerine")
	if got, name := activeAccent("208"); got != lipgloss.Color("208") || name != "208" {
		t.Errorf("a bad override resolved to %v (%q), want the preference underneath it", got, name)
	}
}

// The decision area 16b had to make before building: one accent, three roles.
// The title, the checked-out branch marker and the selected row's marker move
// together because they mean one thing — "Drift is pointing at this". Three
// separate settings would have been three ways to make an incoherent screen.
func TestOneAccentServesAllThreeRoles(t *testing.T) {
	t.Setenv("DRIFT_ACCENT", "")
	t.Setenv("DRIFT_BAND", store.SelectionPair) // the default, and the one with a marker

	def := newStyles(store.Prefs{})
	if def.title.GetForeground() != def.marker.GetForeground() {
		t.Error("the title and the checked-out marker have drifted apart by default")
	}
	if def.band.accent != def.title.GetForeground() {
		t.Error("the selection marker is not the same accent as the title by default")
	}

	themed := newStyles(store.Prefs{Accent: "208"})
	want := lipgloss.Color("208")
	for role, got := range map[string]lipgloss.TerminalColor{
		"title":            themed.title.GetForeground(),
		"checked-out":      themed.marker.GetForeground(),
		"selection marker": themed.band.accent,
	} {
		if got != want {
			t.Errorf("%s = %v, want the themed accent %v", role, got, want)
		}
	}
}

// The other half of that decision, and the one with a wrong answer: colour is
// the signal (DESIGN.md §1), so the roles that carry an alarm are not on offer.
// `behind` is the one thing on screen that shouts and `unmergeable` is a
// distinct alarm beside it; a preference that let two of those collide would
// not be a preference, it would be a broken screen.
func TestThemingLeavesTheAlarmRolesAlone(t *testing.T) {
	t.Setenv("DRIFT_ACCENT", "")
	t.Setenv("DRIFT_BAND", "")

	def := newStyles(store.Prefs{})
	themed := newStyles(store.Prefs{Accent: "208"})

	roles := map[string][2]lipgloss.Style{
		"behind":   {def.behind, themed.behind},   // the one alarm that matters
		"unmerge":  {def.unmerge, themed.unmerge}, // a distinct alarm beside it
		"dirty":    {def.dirty, themed.dirty},
		"errText":  {def.errText, themed.errText},
		"branch":   {def.branch, themed.branch},
		"ahead":    {def.ahead, themed.ahead},
		"sync":     {def.sync, themed.sync},
		"target":   {def.target, themed.target},
		"help":     {def.help, themed.help},
		"hint":     {def.hint, themed.hint},
		"diffAdd":  {def.diffAdd, themed.diffAdd},
		"diffDel":  {def.diffDel, themed.diffDel},
		"diffHunk": {def.diffHunk, themed.diffHunk},
		"diffMeta": {def.diffMeta, themed.diffMeta},
	}
	for name, pair := range roles {
		if pair[0].GetForeground() != pair[1].GetForeground() {
			t.Errorf("%s changed with the accent — only the accent role is themable", name)
		}
	}

	// The panel border too: it is not an alarm, but it is not the accent
	// either, and a border's whole job is to be found without being read.
	if def.panel.GetBorderTopForeground() != themed.panel.GetBorderTopForeground() {
		t.Error("the panel border changed with the accent")
	}
}

// The wiring, end to end. Resolving an accent correctly and then dropping it on
// the way to a screen is the failure a unit test on activeAccent cannot see,
// and it is a one-argument mistake away at every seam — the same reason the
// saved *selection* has this test.
func TestASavedAccentReachesTheScreen(t *testing.T) {
	t.Setenv("DRIFT_ACCENT", "")
	t.Setenv("DRIFT_BAND", "")
	want := lipgloss.Color("208")

	m := New(git.New(t_nowhere), sampleConfig(), sampleStore(), store.Prefs{Accent: "208"})
	if got := m.styles.title.GetForeground(); got != want {
		t.Errorf("the dashboard rendered %v, want the preference", got)
	}

	// The wizard is a separate program with its own styles, and the same
	// preference has to hold there: one run renders one palette everywhere.
	w := newWizard(remoteBranches("origin/main"), store.Prefs{Accent: "208"})
	if got := w.styles.title.GetForeground(); got != want {
		t.Errorf("the wizard rendered %v, want the preference", got)
	}
}

// DRIFT_BG was an instrument for the adaptive palette's silent failure mode,
// and area 16b gave it a file: a terminal Lip Gloss misdetects is misdetected
// every run, so "for this run" is the wrong shape for the fix. Same order as
// everything else — the env var still outranks the file.
func TestBackgroundResolutionOrder(t *testing.T) {
	t.Setenv("DRIFT_BG", "")
	if got := backgroundOverride(""); got != "" {
		t.Errorf("nothing set forced %q, want detection left alone", got)
	}
	if got := backgroundOverride(store.BackgroundLight); got != store.BackgroundLight {
		t.Errorf("a saved background resolved to %q, want light", got)
	}

	t.Setenv("DRIFT_BG", "dark")
	if got := backgroundOverride(store.BackgroundLight); got != store.BackgroundDark {
		t.Errorf("DRIFT_BG lost to the preference: got %q", got)
	}

	t.Setenv("DRIFT_BG", "auto")
	if got := backgroundOverride(store.BackgroundLight); got != store.BackgroundLight {
		t.Errorf("a bad override resolved to %q, want the preference underneath it", got)
	}
}

// And it actually forces the renderer, which is the whole point: every adaptive
// value resolves against what Lip Gloss believes at the moment it is used, so a
// preference that parsed correctly and changed nothing would be indistinguishable
// from detection having been right all along.
func TestASavedBackgroundForcesTheEnd(t *testing.T) {
	restoreBackground(t)
	t.Setenv("DRIFT_BG", "")

	newStyles(store.Prefs{Background: store.BackgroundLight})
	if lipgloss.HasDarkBackground() {
		t.Error("a saved light background did not reach the renderer")
	}
	newStyles(store.Prefs{Background: store.BackgroundDark})
	if !lipgloss.HasDarkBackground() {
		t.Error("a saved dark background did not reach the renderer")
	}
}

// The title reports what an override actually resolved to, and stays silent
// otherwise. A default install must never carry diagnostics in its title, and a
// saved preference is a decision already made — the label belongs to a run being
// experimented on.
func TestOverrideLabelNamesWhatIsInForce(t *testing.T) {
	restoreBackground(t) // the saved-preference case below forces an end
	t.Setenv("DRIFT_BAND", "")
	t.Setenv("DRIFT_ACCENT", "")
	t.Setenv("DRIFT_BG", "")

	if got := overrideLabel(newStyles(store.Prefs{
		Selection:  store.SelectionMarker,
		Accent:     "208",
		Background: store.BackgroundDark,
	})); got != "" {
		t.Errorf("saved preferences labelled the title with %q", got)
	}

	// An accent is named only when it was overridden: it is literally the colour
	// this label is drawn beside, so naming it otherwise is noise about something
	// already on screen. The band is named regardless, because `pair` and
	// `contrast` differ subtly enough that the screen does not tell you which.
	t.Setenv("DRIFT_BAND", store.SelectionMarker)
	got := overrideLabel(newStyles(store.Prefs{Accent: "208"}))
	if !strings.Contains(got, "band:"+store.SelectionMarker) {
		t.Errorf("label %q does not name the treatment in force", got)
	}
	if strings.Contains(got, "accent:") {
		t.Errorf("label %q named an accent nobody overrode", got)
	}
	if !strings.Contains(got, "bg:") {
		t.Errorf("label %q does not say which background is live", got)
	}

	t.Setenv("DRIFT_ACCENT", "#ff8800")
	if got := overrideLabel(newStyles(store.Prefs{})); !strings.Contains(got, "accent:#ff8800") {
		t.Errorf("label %q does not name the overridden accent", got)
	}

	// A typo names what is in force, never what was asked for — otherwise the
	// label would confirm a colour that never rendered. The default is an
	// adaptive pair, which no single value names, so it says so.
	t.Setenv("DRIFT_ACCENT", "tangerine")
	if got := overrideLabel(newStyles(store.Prefs{})); !strings.Contains(got, "accent:default") {
		t.Errorf("label %q does not report the accent actually in force", got)
	}
	if got := overrideLabel(newStyles(store.Prefs{Accent: "208"})); !strings.Contains(got, "accent:208") {
		t.Errorf("label %q does not report the preference a bad override fell through to", got)
	}
}

// restoreBackground puts the renderer back after a test forces an end of the
// palette. SetHasDarkBackground is global state, so a test that leaves it
// flipped hands every later test a palette it did not ask for.
func restoreBackground(t *testing.T) {
	t.Helper()
	was := lipgloss.HasDarkBackground()
	t.Cleanup(func() { lipgloss.SetHasDarkBackground(was) })
}
