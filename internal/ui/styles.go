package ui

import "github.com/charmbracelet/lipgloss"

// Color roles, pinned here on the first build per DESIGN.md §1. Meaning drives
// the choice: behind>0 is the one alarm that matters and reads as a warning;
// everything else is neutral so the alarm stands out. ANSI-256 codes keep the
// palette legible on the widest range of terminals.
//
// Every role is adaptive (roadmap area 15). The palette was pinned against a
// dark terminal and read as fixed ANSI-256, which is two different decisions
// wearing one coat: the *depth* is still right, the *assumption* was not. On a
// light background the dark-terminal values invert — 220 (a bright yellow dirty
// dot) all but vanishes on white, and 236 as a selection band puts the
// terminal's dark default foreground onto a near-black row. Lip Gloss resolves
// AdaptiveColor from the background it detects at startup, so each role now
// names both ends and neither terminal gets the other's palette.
//
// The Dark values are the ones that shipped, unchanged. The Light values are
// their counterparts a few steps darker — the same hue, enough luminance to sit
// against white — so the roles keep their relationship to each other: warning
// still shouts, neutral still recedes.
var (
	colWarning = lipgloss.AdaptiveColor{Light: "166", Dark: "214"} // behind > 0 — the target moved under me
	colNeutral = lipgloss.AdaptiveColor{Light: "240", Dark: "245"} // ahead, and in-sync clusters — quiet
	colDirty   = lipgloss.AdaptiveColor{Light: "172", Dark: "220"} // the dirty dot
	colMarker  = lipgloss.AdaptiveColor{Light: "26", Dark: "39"}   // checked-out branch marker
	colTitle   = lipgloss.AdaptiveColor{Light: "26", Dark: "39"}
	colBorder  = lipgloss.AdaptiveColor{Light: "247", Dark: "240"}
	colFaint   = lipgloss.AdaptiveColor{Light: "244", Dark: "240"} // hints, help line
	colErr     = lipgloss.AdaptiveColor{Light: "160", Dark: "203"} // error text
	colUnmerge = lipgloss.AdaptiveColor{Light: "127", Dark: "170"} // unmergeable collision — reconcile by hand

	// Diff panel. Muted rather than saturated: a whole screen of incoming change
	// is the normal case here, so the +/- pair must read as structure, not as
	// alarm — behind>0 is still the only thing on screen shouting.
	colDiffAdd  = lipgloss.AdaptiveColor{Light: "28", Dark: "71"}   // added line
	colDiffDel  = lipgloss.AdaptiveColor{Light: "124", Dark: "167"} // removed line
	colDiffHunk = lipgloss.AdaptiveColor{Light: "66", Dark: "109"}  // @@ hunk header — where you are in the file
)

// The border is a shade fainter on a light terminal than the hint text is, and
// on a dark terminal they are the same value. That is deliberate rather than an
// oversight: on white, 240 draws a panel outline heavy enough to compete with
// the rows inside it, and the border's whole job is to be found without being
// read.

// styles bundles the reusable Lip Gloss styles. Built once and carried on the
// Model so View allocates nothing per frame.
type styles struct {
	app       lipgloss.Style
	panel     lipgloss.Style
	title     lipgloss.Style
	ticket    lipgloss.Style
	ticketSel lipgloss.Style
	selMark   lipgloss.Style
	branch    lipgloss.Style
	behind    lipgloss.Style
	ahead     lipgloss.Style
	sync      lipgloss.Style
	dirty     lipgloss.Style
	marker    lipgloss.Style
	target    lipgloss.Style
	help      lipgloss.Style
	hint      lipgloss.Style
	errText   lipgloss.Style
	unmerge   lipgloss.Style
	diffAdd   lipgloss.Style
	diffDel   lipgloss.Style
	diffHunk  lipgloss.Style
	diffMeta  lipgloss.Style

	// band is the selected-row treatment in force. Carried on styles rather
	// than read from the environment at each use, so one run renders one
	// treatment everywhere — the dashboard, the wizard and every overlay.
	band bandTreatment
}

func newStyles() styles {
	applyBackgroundOverride()
	band := activeBand()
	return styles{
		app: lipgloss.NewStyle().Padding(0, 1),
		panel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colBorder).
			Padding(0, 1),
		title: lipgloss.NewStyle().
			Foreground(colTitle).
			Bold(true),
		ticket:    lipgloss.NewStyle(),
		ticketSel: band.fillStyle(),
		selMark:   band.markerStyle(),
		branch:    lipgloss.NewStyle().Foreground(colNeutral),
		behind:    lipgloss.NewStyle().Foreground(colWarning).Bold(true),
		ahead:     lipgloss.NewStyle().Foreground(colNeutral),
		sync:      lipgloss.NewStyle().Foreground(colFaint),
		dirty:     lipgloss.NewStyle().Foreground(colDirty),
		marker:    lipgloss.NewStyle().Foreground(colMarker),
		target:    lipgloss.NewStyle().Foreground(colNeutral),
		help:      lipgloss.NewStyle().Foreground(colFaint),
		hint:      lipgloss.NewStyle().Foreground(colFaint).Italic(true),
		errText:   lipgloss.NewStyle().Foreground(colErr),
		unmerge:   lipgloss.NewStyle().Foreground(colUnmerge).Bold(true),
		diffAdd:   lipgloss.NewStyle().Foreground(colDiffAdd),
		diffDel:   lipgloss.NewStyle().Foreground(colDiffDel),
		diffHunk:  lipgloss.NewStyle().Foreground(colDiffHunk).Bold(true),
		diffMeta:  lipgloss.NewStyle().Foreground(colFaint),
		band:      band,
	}
}

// titleText is the app title, carrying the active selection treatment while the
// area-15 harness is being driven and nothing once DRIFT_BAND is unset. It is
// one helper so the dashboard header and the wizard's own title agree, and so
// the label is measured by whatever truncation each of them already applies.
func titleText(s styles) string {
	t := s.title.Render("drift")
	if label := bandLabel(s.band); label != "" {
		t += "  " + s.help.Render(label)
	}
	return t
}
