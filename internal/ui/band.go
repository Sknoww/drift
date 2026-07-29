package ui

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
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
// DRIFT_BAND selects between them, which is temporary: a selection style is a
// per-user preference, and its home is the user-global config root (roadmap
// area 16). Until that lands the environment is the only way in, and it is
// deliberately undocumented in the README.
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
// Every colour is adaptive. That is not a flourish but the second half of the
// pass — a treatment judged only against a dark terminal is exactly how the
// original one got here. ANSI-256 stays right (DESIGN.md §1); this is about
// light versus dark, not colour depth.
var bandTreatments = []bandTreatment{
	{
		name:   "pair",
		desc:   "a subtle band under a left-edge marker (fzf, Telescope) — the default",
		fill:   true,
		marker: true,
		bold:   true,
		bg:     lipgloss.AdaptiveColor{Light: "254", Dark: "237"},
		fg:     lipgloss.AdaptiveColor{Light: "232", Dark: "255"},
		accent: lipgloss.AdaptiveColor{Light: "26", Dark: "39"},
	},
	{
		name: "contrast",
		desc: "the same idea, raised: a grey that actually reads, both ends pinned",
		fill: true,
		bold: true,
		bg:   lipgloss.AdaptiveColor{Light: "250", Dark: "242"},
		fg:   lipgloss.AdaptiveColor{Light: "232", Dark: "255"},
	},
	{
		name: "accent",
		desc: "an accent hue rather than a lighter grey — the lazygit/k9s shape",
		fill: true,
		bold: true,
		bg:   lipgloss.AdaptiveColor{Light: "153", Dark: "24"},
		fg:   lipgloss.AdaptiveColor{Light: "232", Dark: "255"},
	},
	{
		name:   "marker",
		desc:   "left-edge ▌ only, no background at all — deletes reopenBand",
		marker: true,
		accent: lipgloss.AdaptiveColor{Light: "26", Dark: "39"},
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

// activeBand reads the treatment to render from DRIFT_BAND.
//
// Unset — and unknown, which is a typo — is the default, so a run with nothing
// set gets the treatment that was chosen rather than a failure. A typo is not
// silent: while any override is set the title carries the treatment actually in
// force (bandLabel), so what is on screen always names itself.
func activeBand() bandTreatment {
	want := strings.TrimSpace(os.Getenv("DRIFT_BAND"))
	for _, t := range bandTreatments {
		if t.name == want {
			return t
		}
	}
	return bandTreatments[0]
}

// bandLabel is the suffix the title carries while either override is set, and
// empty on an ordinary run — a default install must never carry diagnostics in
// its title. It names the treatment actually resolved, not the string asked
// for, so a typo reads as the default rather than as the treatment the user
// thought they had selected.
//
// It carries the detected background alongside it, because that is the half of
// the palette work with a silent failure mode: every Light value is inert if
// Lip Gloss decides the terminal is dark when it is not, and an adaptive
// palette that never adapts looks exactly like one whose light values are badly
// chosen. Naming the detected end tells those two apart from the screen.
func bandLabel(t bandTreatment) string {
	if strings.TrimSpace(os.Getenv("DRIFT_BAND")) == "" &&
		strings.TrimSpace(os.Getenv("DRIFT_BG")) == "" {
		return ""
	}
	bg := "light"
	if lipgloss.HasDarkBackground() {
		bg = "dark"
	}
	return "band:" + t.name + " · bg:" + bg
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
// Only ever acts when DRIFT_BG is set, so it is inert on a normal run and in
// tests, neither of which should be touching global renderer state.
func applyBackgroundOverride() {
	switch strings.TrimSpace(os.Getenv("DRIFT_BG")) {
	case "light":
		lipgloss.SetHasDarkBackground(false)
	case "dark":
		lipgloss.SetHasDarkBackground(true)
	}
}
