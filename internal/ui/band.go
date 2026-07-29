package ui

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Sknoww/drift/internal/store"
)

// Selection treatments — how the cursor's row is drawn (roadmap area 15).
//
// The measurement that opened this: the original band was ANSI 236 =
// rgb(48,48,48), which against common terminal backgrounds gives a contrast
// ratio of 1.06:1 on One Dark, 1.08 Dracula, 1.12 Gruvbox, 1.26 VS Code Dark
// and 1.59 on pure black — its best case. The selected row was not badly
// designed so much as almost not drawn. Alongside it, the style set a
// background and *no* foreground, so on a light terminal the default dark
// foreground landed on a near-black band: the one thing that must always be
// legible was the thing that disappeared. One style, two symptoms — hence one
// pass, and hence every treatment below pins both ends.
//
// Which one to draw could not be settled by argument, so it was settled by
// looking, on a dark theme and a light one. "pair" won and is the default:
// a subtle band under a left-edge marker, the shape fzf and Telescope both
// use. It degrades best, which is the property that decided it — where a band
// alone is at the mercy of the terminal's background, the marker still finds
// the row when the band is nearly invisible.
//
// Keeping the band *and* adding a marker is a deviation from the documented
// band-only decision and from the argument that a marker would let reopenBand
// (view.go) be deleted: see docs/adr/0001-selection-band-and-marker.md.
//
// The original 236 band is deliberately not among the candidates. It is the
// defect this pass exists to fix, and offering it as a choice would ship it as
// a supported option.
//
// Which one is drawn is a per-user preference, so its home is the user-global
// prefs.json (roadmap area 16a) and the names are store's — see
// store.SelectionPair and friends. DRIFT_BAND overrides the file for one run
// and is now documented rather than a temporary harness: trying a treatment
// against your own repo is how the choice gets made in the first place, and an
// env var says "for this run" in a way an edited file cannot.
const bandMarkerGlyph = "▌"

// bandTreatment is one candidate rendering of the selected row.
//
// fill and marker are not exclusive: fzf and Telescope both pair a subtle band
// with a left-edge marker, which is what "pair" below is.
type bandTreatment struct {
	name string
	desc string

	fill   bool // paint a background band across the panel's full inner width
	marker bool // draw a left-edge marker in the row's own gutter
	bold   bool

	bg     lipgloss.TerminalColor // band background, when fill
	fg     lipgloss.TerminalColor // band foreground, when fill — pinned, never inherited
	accent lipgloss.TerminalColor // the marker's colour, when marker
}

// bandTreatments are the supported selection styles, the default first.
//
// A treatment is a **shape** — does it fill, does it mark — and theming
// (roadmap area 16b) is the palette poured into it. That split is why the
// marker treatments name no accent here: theirs is resolved per run in
// theme.go, so `pair` in someone else's accent is the same shape with a
// different value rather than a fifth hardcoded entry in this list.
//
// The fill colours stay baked, and that is the split holding rather than an
// omission. A band is a *background*, so it is only ever half a decision: it
// needs a foreground pinned against it (the light-terminal defect this whole
// pass exists to fix), and one user-supplied value cannot pin the pair. Note
// that store.SelectionAccent — a band in an accent *hue* — is therefore
// unrelated to the accent preference, which is a foreground signal. Two things
// with one word, and the README says so.
//
// Every colour here is adaptive. That is not a flourish but the second half of
// the pass — a treatment judged only against a dark terminal is exactly how the
// original one got here. ANSI-256 stays right (DESIGN.md §1); this is about
// light versus dark, not colour depth.
var bandTreatments = []bandTreatment{
	{
		name:   store.SelectionPair,
		desc:   "a subtle band under a left-edge marker (fzf, Telescope) — the default",
		fill:   true,
		marker: true,
		bold:   true,
		bg:     lipgloss.AdaptiveColor{Light: "254", Dark: "237"},
		fg:     lipgloss.AdaptiveColor{Light: "232", Dark: "255"},
	},
	{
		name: store.SelectionContrast,
		desc: "the same idea, raised: a grey that actually reads, both ends pinned",
		fill: true,
		bold: true,
		bg:   lipgloss.AdaptiveColor{Light: "250", Dark: "242"},
		fg:   lipgloss.AdaptiveColor{Light: "232", Dark: "255"},
	},
	{
		name: store.SelectionAccent,
		desc: "a fixed accent hue rather than a lighter grey — the lazygit/k9s shape",
		fill: true,
		bold: true,
		bg:   lipgloss.AdaptiveColor{Light: "153", Dark: "24"},
		fg:   lipgloss.AdaptiveColor{Light: "232", Dark: "255"},
	},
	{
		name:   store.SelectionMarker,
		desc:   "left-edge ▌ only, no background at all — deletes reopenBand",
		marker: true,
	},
}

// gutter is the width every row reserves at its left edge, so the marker has
// somewhere to go that is not on top of the row's first column.
//
// It comes out of rowWidth rather than being prefixed after the fact: a row
// built to the full content width and then pushed two cells right overflows,
// and clipRow would cut the trailing status cluster — which would damn the
// marker treatments for a cost they do not actually have. The columns absorb it
// exactly as they absorb a narrower terminal (DESIGN.md §1).
func (t bandTreatment) gutter() int {
	if !t.marker {
		return 0
	}
	return 2 // the glyph plus one space
}

// fillStyle is the band itself. A treatment that does not fill returns the zero
// style and selectBand skips banding entirely.
//
// The foreground is pinned whenever there is a background, which is the whole
// light-terminal fix: a row's cells that carry their own colour keep it, and the
// ones that do not (a ticket headline is plain by design) stop inheriting the
// terminal's default foreground onto a band that may be the same shade as it.
func (t bandTreatment) fillStyle() lipgloss.Style {
	st := lipgloss.NewStyle()
	if !t.fill {
		return st
	}
	if t.bg != nil {
		st = st.Background(t.bg)
	}
	if t.fg != nil {
		st = st.Foreground(t.fg)
	}
	return st.Bold(t.bold)
}

// markerStyle colours the left-edge glyph.
func (t bandTreatment) markerStyle() lipgloss.Style {
	st := lipgloss.NewStyle()
	if t.accent != nil {
		st = st.Foreground(t.accent)
	}
	return st
}

// activeBand resolves the treatment to render: DRIFT_BAND, then the preference
// read from prefs.json, then the default. The env var wins because it means
// "for this run" — trying a treatment must not require editing the file you are
// trying to decide the contents of.
//
// An unrecognised name falls through to the next source rather than failing,
// which reads differently at each level and is meant to. A bad *preference*
// never reaches here at all: store.LoadPrefs validates the file and refuses to
// start, since a typo silently rendering the default is indistinguishable from
// the requested treatment working. A bad DRIFT_BAND is a shell typo in a
// throwaway override, and it is not silent either — while any override is set
// the title carries the treatment actually in force (overrideLabel), so what is
// on screen always names itself.
//
// The accent is applied here rather than stored on the treatment, so the shape
// and its colour are resolved in one place and a marker treatment can never
// reach a screen with an unset glyph colour.
func activeBand(pref string, accent lipgloss.TerminalColor) bandTreatment {
	t := bandTreatments[0]
	if named, ok := bandNamed(os.Getenv("DRIFT_BAND")); ok {
		t = named
	} else if named, ok := bandNamed(pref); ok {
		t = named
	}
	if t.marker {
		t.accent = accent
	}
	return t
}

// bandNamed looks a treatment up by name, reporting whether it exists. An empty
// name is simply an unset source, not a miss worth distinguishing.
func bandNamed(name string) (bandTreatment, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return bandTreatment{}, false
	}
	for _, t := range bandTreatments {
		if t.name == name {
			return t, true
		}
	}
	return bandTreatment{}, false
}

// The title's override label and the background override both moved to
// theme.go when area 16b split shape from palette: neither is about the
// selected row, and the label now reports an accent this file no longer owns.
