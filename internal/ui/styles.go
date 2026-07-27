package ui

import "github.com/charmbracelet/lipgloss"

// Color roles, pinned here on the first build per DESIGN.md §1. Meaning drives
// the choice: behind>0 is the one alarm that matters and reads as a warning;
// everything else is neutral so the alarm stands out. ANSI-256 codes keep the
// palette legible on the widest range of terminals.
var (
	colWarning = lipgloss.Color("214") // behind > 0 — the target moved under me
	colNeutral = lipgloss.Color("245") // ahead, and in-sync clusters — quiet
	colDirty   = lipgloss.Color("220") // the dirty dot
	colMarker  = lipgloss.Color("39")  // checked-out branch marker
	colTitle   = lipgloss.Color("39")
	colBorder  = lipgloss.Color("240")
	colFaint   = lipgloss.Color("240") // hints, help line
	colSelBG   = lipgloss.Color("236") // selected row band
	colErr     = lipgloss.Color("203") // error text
	colUnmerge = lipgloss.Color("170") // unmergeable collision — reconcile by hand

	// Diff panel. Muted rather than saturated: a whole screen of incoming change
	// is the normal case here, so the +/- pair must read as structure, not as
	// alarm — behind>0 is still the only thing on screen shouting.
	colDiffAdd  = lipgloss.Color("71")  // added line
	colDiffDel  = lipgloss.Color("167") // removed line
	colDiffHunk = lipgloss.Color("109") // @@ hunk header — where you are in the file
)

// styles bundles the reusable Lip Gloss styles. Built once and carried on the
// Model so View allocates nothing per frame.
type styles struct {
	app       lipgloss.Style
	panel     lipgloss.Style
	title     lipgloss.Style
	ticket    lipgloss.Style
	ticketSel lipgloss.Style
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
}

func newStyles() styles {
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
		ticketSel: lipgloss.NewStyle().Background(colSelBG).Bold(true),
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
	}
}
