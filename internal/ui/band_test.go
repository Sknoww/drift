package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// The selection treatments are a prototype harness (band.go), but the geometry
// they have to keep is not provisional: whichever one is chosen, a selected row
// still has to fit the panel it is drawn in. These assert the properties that
// must survive the choice, so picking one by eye cannot quietly reintroduce a
// failure areas 14 and 15 already fixed.

func TestActiveBandResolvesByName(t *testing.T) {
	for _, want := range bandTreatments {
		t.Setenv("DRIFT_BAND", want.name)
		if got := activeBand(); got.name != want.name {
			t.Errorf("DRIFT_BAND=%q selected %q", want.name, got.name)
		}
	}

	// The default is what an ordinary run gets, and a typo falls back to it
	// rather than to some third thing.
	if bandTreatments[0].name != "pair" {
		t.Errorf("the default treatment is %q, want pair", bandTreatments[0].name)
	}
	t.Setenv("DRIFT_BAND", "")
	if got := activeBand(); got.name != bandTreatments[0].name {
		t.Errorf("unset DRIFT_BAND selected %q, want the default", got.name)
	}
	t.Setenv("DRIFT_BAND", "nonesuch")
	if got := activeBand(); got.name != bandTreatments[0].name {
		t.Errorf("unknown DRIFT_BAND selected %q, want the default", got.name)
	}
	if got := bandLabel(activeBand()); !strings.HasPrefix(got, "band:"+bandTreatments[0].name) {
		t.Errorf("a typo must still name the treatment actually in force, got %q", got)
	}
	// The label also states which end of the palette is live — the one thing
	// about the adaptive half with a silent failure mode.
	if got := bandLabel(activeBand()); !strings.Contains(got, "bg:") {
		t.Errorf("label %q does not say which background was detected", got)
	}

	// With nothing set the title carries no label at all: the harness leaves no
	// trace on a normal run.
	t.Setenv("DRIFT_BAND", "")
	if got := bandLabel(activeBand()); got != "" {
		t.Errorf("bandLabel with DRIFT_BAND unset = %q, want empty", got)
	}
}

// rowWidth is what stops a marker treatment from appearing to cost a signal it
// does not cost: rows must be built two cells narrower, not built full-width and
// then shoved right for clipRow to cut.
func TestRowWidthReservesTheSelectionGutter(t *testing.T) {
	const width = 100

	t.Setenv("DRIFT_BAND", "contrast")
	band := newStyles()
	if band.band.gutter() != 0 {
		t.Fatal("precondition: a fill-only treatment should reserve no gutter")
	}
	if got, want := rowWidth(band, width), contentWidth(band, width); got != want {
		t.Errorf("a band treatment reserves nothing: rowWidth = %d, want %d", got, want)
	}

	t.Setenv("DRIFT_BAND", "marker")
	mark := newStyles()
	if mark.band.gutter() == 0 {
		t.Fatal("precondition: the marker treatment should reserve a gutter")
	}
	if got, want := rowWidth(mark, width), contentWidth(mark, width)-mark.band.gutter(); got != want {
		t.Errorf("rowWidth = %d, want the panel less the gutter %d", got, want)
	}

	// Size unknown stays unknown — callers fall back to natural sizing rather
	// than to a guess two cells narrower than one.
	if got := rowWidth(mark, 0); got != 0 {
		t.Errorf("rowWidth before the first WindowSizeMsg = %d, want 0", got)
	}
}

// The acceptance property, asserted for every treatment: a windowed list's
// lines all fit the panel, including the selected one. A row that overflows
// wraps, and a wrapped row is how the frame ran off the top of the terminal in
// area 14.
func TestEveryTreatmentKeepsTheSelectedRowInsideThePanel(t *testing.T) {
	for _, tr := range bandTreatments {
		t.Run(tr.name, func(t *testing.T) {
			t.Setenv("DRIFT_BAND", tr.name)
			s := newStyles()

			const width, height = 100, 24
			cw := contentWidth(s, width)
			rw := rowWidth(s, width)

			// Rows built to exactly what a row has to spend, as every screen's
			// column budget builds them.
			rows := make([]string, 40)
			for i := range rows {
				rows[i] = fit(strings.Repeat("x", rw+20), rw)
			}

			for _, cursor := range []int{0, 7, len(rows) - 1} {
				out := listBody(s, width, height, []string{"header"}, rows, cursor)
				for i, line := range strings.Split(out, "\n") {
					if w := lipgloss.Width(line); w > cw {
						t.Errorf("cursor %d: line %d is %d wide, panel is %d", cursor, i, w, cw)
					}
				}
			}
		})
	}
}

// A marker treatment marks the cursor's row and only the cursor's row, and every
// other row keeps its columns aligned with it — which is why the blank gutter is
// drawn on all of them rather than the marker being prefixed to one.
func TestMarkerTreatmentMarksOnlyTheSelectedRow(t *testing.T) {
	t.Setenv("DRIFT_BAND", "marker")
	s := newStyles()

	rows := []string{"alpha", "bravo", "charlie"}
	lines := strings.Split(listBody(s, 100, 24, nil, rows, 1), "\n")
	if len(lines) != len(rows) {
		t.Fatalf("got %d lines for %d rows", len(lines), len(rows))
	}

	for i, line := range lines {
		marked := strings.Contains(line, bandMarkerGlyph)
		if want := i == 1; marked != want {
			t.Errorf("line %d marked = %v, want %v: %q", i, marked, want, line)
		}
	}

	first, second := lipgloss.Width(lines[0]), lipgloss.Width(lines[1])
	if first != second {
		t.Errorf("marked row is %d wide, unmarked %d — the gutter must be on every row", second, first)
	}
	if !strings.HasPrefix(lines[0], strings.Repeat(" ", s.band.gutter())) {
		t.Error("an unselected row is missing its blank gutter")
	}
}

// The light-terminal half of the pass. A background with no foreground is the
// defect that started it — on a light terminal the default foreground is dark,
// so the band renders dark-on-dark and the one thing that must always be legible
// disappears. No treatment is exempt: the original 236 band is not a candidate
// precisely because it is the defect, and this is what stops it coming back
// under another name.
func TestFillTreatmentsPinBothEnds(t *testing.T) {
	for _, tr := range bandTreatments {
		if !tr.fill {
			continue
		}
		if tr.bg == nil || tr.fg == nil {
			t.Errorf("%s: fill treatment must set both a background and a foreground", tr.name)
		}
	}
	for _, tr := range bandTreatments {
		if tr.marker && tr.accent == nil {
			t.Errorf("%s: marker treatment must colour its glyph", tr.name)
		}
	}
}

// Every colour role names a light end and a dark end. The palette was pinned
// against a dark terminal and read as fixed; a role with one end is how that
// happens again.
func TestPaletteRolesNameBothBackgrounds(t *testing.T) {
	roles := map[string]lipgloss.AdaptiveColor{
		"warning":  colWarning,
		"neutral":  colNeutral,
		"dirty":    colDirty,
		"marker":   colMarker,
		"title":    colTitle,
		"border":   colBorder,
		"faint":    colFaint,
		"err":      colErr,
		"unmerge":  colUnmerge,
		"diffAdd":  colDiffAdd,
		"diffDel":  colDiffDel,
		"diffHunk": colDiffHunk,
	}
	for name, c := range roles {
		if c.Light == "" || c.Dark == "" {
			t.Errorf("%s: both ends must be named, got light=%q dark=%q", name, c.Light, c.Dark)
		}
	}
}
