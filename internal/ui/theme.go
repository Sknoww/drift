package ui

import (
	"os"

	"github.com/charmbracelet/lipgloss"

	"github.com/Sknoww/drift/internal/store"
)

// Theming — resolving the palette for one run (roadmap area 16b).
//
// band.go answers "what *shape* is the selected row"; this file answers "what
// colours go into it". Area 15 shipped four treatments with their colours baked
// in, which conflates the two: `pair` in someone else's accent had to be a fifth
// hardcoded treatment rather than the same shape with a different value poured
// in. Splitting them is what this is.
//
// Exactly one role is themable, and the area title says which: the accent. It
// is the one the dogfooding complaint was actually about ("the marker reads
// well and the blue is not to taste"), and it is the only role that carries no
// meaning a user could break by recolouring it. See store.Prefs.Accent for why
// the alarm roles are not on offer.

// activeAccent resolves the accent to render, and names it for the title.
//
// The order is the one prefs.json established for the selection treatment:
// DRIFT_ACCENT, then the saved preference, then Drift's own default. An env var
// means "for this run", which is exactly what trying a colour against your own
// repo needs and exactly what editing the file you are trying to decide the
// contents of cannot say.
//
// The returned name is what the value resolved *to*, never what was asked for,
// so a typo reads as the default rather than as the colour the user thought
// they had chosen. It is empty only when a colour cannot be named by one value
// — which is precisely the default, an adaptive pair.
//
// A bad DRIFT_ACCENT falls through rather than failing, the same asymmetry
// activeBand documents: a typo in the file refuses to start (store.LoadPrefs
// validates it), because a preference silently rendering the default is
// indistinguishable from it working, while a typo in a throwaway shell override
// is not worth refusing over — and is not silent either, since the title names
// the accent actually in force whenever an override is set.
func activeAccent(pref string) (lipgloss.TerminalColor, string) {
	if v, ok := store.ParseAccent(os.Getenv("DRIFT_ACCENT")); ok {
		return lipgloss.Color(v), v
	}
	if v, ok := store.ParseAccent(pref); ok {
		return lipgloss.Color(v), v
	}
	return colAccent, ""
}

// backgroundOverride is which end of the adaptive palette has been forced, or
// empty to let Lip Gloss detect it: DRIFT_BG, then the saved preference.
//
// Folding DRIFT_BG into prefs.json is area 16b's doing, and the argument is the
// same one that gave the selection style a file. The env var exists because
// detection has a silent failure mode, and "for this run" is the right shape for
// *diagnosing* that — but a terminal Lip Gloss misdetects is misdetected every
// run, and telling that user to keep an export in their shell rc is offering
// them a diagnostic where they need a setting.
func backgroundOverride(pref string) string {
	if v, ok := store.ParseBackground(os.Getenv("DRIFT_BG")); ok {
		return v
	}
	if v, ok := store.ParseBackground(pref); ok {
		return v
	}
	return ""
}

// applyBackgroundOverride forces which end of the palette is used, for the one
// question a single terminal cannot otherwise answer.
//
// Judging the light values properly means actually switching the terminal's
// theme — a light palette rendered against a dark background says nothing about
// legibility, which is the whole question. What this is for is the failure
// *underneath* that: confirming detection works at all, and letting the light
// values be read off the screen without hunting for them in the source.
//
// It touches global renderer state, so it only ever acts when an override is
// actually set: inert on a normal run, and inert in every test that does not
// ask for it.
func applyBackgroundOverride(pref string) {
	switch backgroundOverride(pref) {
	case store.BackgroundLight:
		lipgloss.SetHasDarkBackground(false)
	case store.BackgroundDark:
		lipgloss.SetHasDarkBackground(true)
	}
}

// overrideLabel is the suffix the title carries while an environment override is
// set, and empty on an ordinary run — a default install must never carry
// diagnostics in its title.
//
// A preference saved in prefs.json deliberately does *not* light it up, on any
// of the three fields. The label is for a run being experimented on; a saved
// preference is a decision already made, and stamping it on the title of every
// run afterwards would be noise about something the user is no longer asking.
//
// The band is always named and the accent only when overridden, which is not an
// inconsistency: `pair` and `contrast` differ subtly enough that the screen does
// not tell you which one you got, whereas an accent is literally the colour this
// label is drawn beside. Naming a colour already on screen is the noise the
// prefs rule above exists to avoid.
//
// The detected background is always named, because that is the half of the
// palette with a silent failure mode: every Light value is inert if Lip Gloss
// decides the terminal is dark when it is not, and an adaptive palette that
// never adapts looks exactly like one whose light values were badly chosen.
func overrideLabel(s styles) string {
	if !anyOverrideSet() {
		return ""
	}
	label := "band:" + s.band.name
	if os.Getenv("DRIFT_ACCENT") != "" {
		label += " · accent:" + accentLabel(s.accentName)
	}
	return label + " · bg:" + detectedBackground()
}

func anyOverrideSet() bool {
	for _, key := range []string{"DRIFT_BAND", "DRIFT_ACCENT", "DRIFT_BG"} {
		if _, set := os.LookupEnv(key); set && os.Getenv(key) != "" {
			return true
		}
	}
	return false
}

// accentLabel names the accent in force. The default has no single value to
// name — it is an adaptive pair — so it says so rather than reporting one of
// its two ends as though it were the whole answer.
func accentLabel(name string) string {
	if name == "" {
		return "default"
	}
	return name
}

func detectedBackground() string {
	if lipgloss.HasDarkBackground() {
		return store.BackgroundDark
	}
	return store.BackgroundLight
}
