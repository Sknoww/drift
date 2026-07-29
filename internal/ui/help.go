package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
)

// The `?` overlay: what the keys do, and what the glyphs mean.
//
// The key half is **generated from the live keymap**, never hand-written. Named
// actions are the contract (DESIGN.md §3), so the help is a view of whatever
// bindings are actually in force: a rebind (area 12) updates it with no code
// change, and it can never drift from what the keys really do — the failure mode
// of every hand-maintained help screen.
//
// The glyph half is static, because glyphs are not rebindable. It exists because
// the status cluster is dense by design and a dot is not self-describing.

// helpEntry is one row of the key table: every key bound to an action, and what
// that action does.
type helpEntry struct {
	keys string
	what string
}

// actionText is what each action is called in the overlay. Deliberately worded
// for the reader ("expand a ticket · open a branch's diff"), not named after the
// function that runs it.
var actionText = map[Action]string{
	ActionMoveUp:          "move up",
	ActionMoveDown:        "move down",
	ActionToggleExpand:    "expand a ticket · open a branch's diff",
	ActionConfirm:         "confirm this screen",
	ActionCancel:          "back out one step",
	ActionToggleCandidate: "include / exclude the selected row",
	ActionFilter:          "narrow the list — type to match, esc clears",
	ActionOpenPicker:      "choose a target for it",
	ActionEditKey:         "rename the target key",
	ActionNextFile:        "next colliding file (wraps)",
	ActionPrevFile:        "previous colliding file (wraps)",
	ActionDeclare:         "declare this file unmergeable to git",
	ActionAdd:             "add a ticket",
	ActionDelete:          "delete the selected ticket",
	ActionRefresh:         "refresh from git",
	ActionFetch:           "fetch, then refresh",
	ActionLocalOnly:       "manage local-only changes",
	ActionHoldLocal:       "hold a working-tree change on this machine",
	ActionRelease:         "stop holding the selected path",
	ActionEditNote:        "note why it's held",
	ActionShelve:          "merge the target into this branch — nothing is published",
	ActionUpdate:          "bring the selected branch up to date and publish it",
	ActionHelp:            "this help",
	ActionQuit:            "quit",
}

// actionOrder is the order rows appear in, grouped by what the reader is doing:
// moving, acting on the selection, then the global verbs. A keymap that binds an
// action missing from this list still shows up — see helpEntries — so adding an
// action can never silently drop it from the help.
var actionOrder = []Action{
	ActionMoveUp, ActionMoveDown, ActionFilter,
	ActionToggleExpand, ActionToggleCandidate, ActionOpenPicker, ActionEditKey,
	ActionNextFile, ActionPrevFile, ActionDeclare,
	ActionHoldLocal, ActionRelease, ActionEditNote,
	ActionConfirm, ActionCancel,
	ActionUpdate, ActionShelve,
	ActionAdd, ActionDelete, ActionRefresh, ActionFetch, ActionLocalOnly,
	ActionHelp, ActionQuit,
}

// keyLabel renames the keys whose Bubble Tea spelling would read badly.
var keyLabel = map[string]string{
	" ":         "space",
	"up":        "↑",
	"down":      "↓",
	"shift+tab": "⇧tab",
}

// helpEntries turns a keymap into display rows, in actionOrder. Every action
// bound in the map appears exactly once, with all of its keys.
func helpEntries(k Keymap) []helpEntry {
	byAction := make(map[Action][]string)
	for key, action := range k {
		byAction[action] = append(byAction[action], key)
	}

	var out []helpEntry
	for _, action := range actionOrder {
		keys, ok := byAction[action]
		if !ok {
			continue
		}
		delete(byAction, action)
		out = append(out, helpEntry{keys: joinKeys(keys), what: describe(action)})
	}

	// Whatever is left is either the parametric pick-target family — nine
	// actions that must read as one row — or an action added without a place in
	// actionOrder, which lands at the end rather than vanishing.
	var picks []string
	var rest []Action
	for action, keys := range byAction {
		if _, ok := pickTargetIndex(action); ok {
			picks = append(picks, keys...)
			continue
		}
		rest = append(rest, action)
	}
	if len(picks) > 0 {
		out = append(out, helpEntry{keys: joinKeys(picks), what: "assign the Nth configured target"})
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i] < rest[j] })
	for _, action := range rest {
		out = append(out, helpEntry{keys: joinKeys(byAction[action]), what: describe(action)})
	}
	return out
}

// describe falls back to the action's own name, so an action with no wording yet
// is visibly unfinished rather than a blank row.
func describe(a Action) string {
	if text, ok := actionText[a]; ok {
		return text
	}
	return string(a)
}

// joinKeys renders an action's keys. A long family (the 1–9 accelerators) is
// shown as a range: nine slash-separated digits is noise, not information.
func joinKeys(keys []string) string {
	labelled := make([]string, len(keys))
	for i, k := range keys {
		if label, ok := keyLabel[k]; ok {
			labelled[i] = label
		} else {
			labelled[i] = k
		}
	}
	// Sorted after labelling, not before, so "j / ↓" and "k / ↑" come out in the
	// same order as each other rather than one of each.
	sort.Strings(labelled)
	if len(labelled) > 3 {
		return labelled[0] + "–" + labelled[len(labelled)-1]
	}
	return strings.Join(labelled, " / ")
}

// glyphLegend explains the status cluster. The dashboard is dense by design
// (DESIGN.md §1), which buys scanning speed at the cost of self-evidence — this
// is where that cost is paid back.
// glyphLegend explains the status cluster, drawing every glyph in the style the
// dashboard actually draws it in — a legend in the wrong color teaches the wrong
// thing, since color *is* the signal here (DESIGN.md §1). It is a method rather
// than a package var precisely so it reads the live styles.
//
// ↓N is shown in its warning style: zero-behind renders faint, but the whole
// reason the row exists is the case where the target moved.
//
// Wording is kept short on purpose — a legend that wraps is harder to read than
// the row it explains — with the reasoning behind each signal left to DESIGN.md.
func (m Model) glyphLegend() []helpEntry {
	s := m.styles
	if m.screen == screenLocalOnly {
		// The local-only list has a legend of its own: its two glyphs say which
		// primitive holds a path, and the tracked one matters most precisely
		// because git hides it everywhere else.
		return []helpEntry{
			{s.dirty.Render("◆"), "tracked — held with skip-worktree, so git status won't show it"},
			{s.help.Render("◇"), "untracked — held with info/exclude, so it's ignored locally"},
		}
	}
	if m.screen == screenShelve && m.shelve.confirm {
		// The prompt draws exactly one glyph, and the report's three are not on
		// screen yet. A legend explains the screen you are on or it teaches the
		// wrong thing — the same rule that has each glyph drawn in its own colour.
		return []helpEntry{
			{s.dirty.Render("●"), "the uncommitted work this is about to stash"},
		}
	}
	if m.screen == screenShelve {
		// The report's own glyphs. The distinction that matters is ■ against ✗:
		// one is git telling you something you have to reconcile, the other is the
		// sequence failing to run at all.
		return []helpEntry{
			{s.sync.Render("✓"), "step done, or the sequence landed clean"},
			{s.unmerge.Render("■"), "stopped and handed back — there is something to reconcile"},
			{s.errText.Render("✗"), "refused or failed before it could finish"},
			{s.unmerge.Render("⚠ unmergeable"), "git can never merge this file — reconcile it in its own tool"},
		}
	}
	return []helpEntry{
		{s.ticket.Render("▸ / ▾"), "ticket collapsed / expanded"},
		{s.behind.Render("↓N"), "commits the target has that you don't — it moved"},
		{s.ahead.Render("↑N"), "commits you have that the target doesn't"},
		// The two below are about the branch's own remote, not the target — the
		// one place on the row where the denominator changes, which is exactly why
		// the wording names origin/<branch> outright.
		{s.dirty.Render("⇡"), "commits not yet on origin/<branch> — u publishes them"},
		{s.help.Render("⊘"), "no upstream — nothing to publish to yet"},
		{s.dirty.Render("●"), "uncommitted changes (checked-out branch only)"},
		{s.marker.Render("▸"), "the branch you have checked out"},
		{s.unmerge.Render("⚠ N unmergeable"), "both sides changed a file git can't merge — enter opens it"},
	}
}

// screenName titles the key table, so it is obvious the list is for where you
// are and not a catalogue of everything Drift can do.
func (m Model) screenName() string {
	switch m.screen {
	case screenAddID:
		return "new ticket"
	case screenPairing:
		if m.add.picker {
			return "target picker"
		}
		return "pairing"
	case screenConfirmDelete:
		return "delete confirmation"
	case screenDiff:
		if m.diff.declare.open {
			return "declare"
		}
		return "diff panel"
	case screenLocalOnly:
		// The candidate picker and the note editor bind no help key, the same as
		// the target picker and the declare overlay: a momentary choice step
		// carries its own one-line help (DESIGN.md §2).
		return "local-only changes"
	case screenShelve:
		if m.shelve.confirm {
			return "stash confirmation"
		}
		// One screen, two verbs: the report names the one that is running, so the
		// help overlay opened over it does too.
		if m.shelve.mode == modeUpdate {
			return "update"
		}
		return "shelve"
	default:
		return "dashboard"
	}
}

// unboundEntries are rows no keymap can produce, because the keys are
// deliberately left unbound so they fall through to a component. Without them
// the help would claim the diff panel has no way to scroll.
func (m Model) unboundEntries() []helpEntry {
	if m.screen == screenDiff && !m.diff.declare.open {
		return []helpEntry{{"j / k / arrows", "scroll the diff (pgup / pgdn too)"}}
	}
	return nil
}

// The overlay scrolls, because it outgrew the terminal (roadmap area 15).
//
// Measured on an 80×24 terminal: the dashboard's overlay was **28 lines** and
// the pairing screen's 26, so the keys it exists to teach were the ones scrolled
// off the top. Area 14's windowing does not apply — there is no cursor to window
// around — but a viewport does, and the diff panel already uses one. It is also
// the only fix that stays fixed: shortening the wording buys back four lines
// once, and areas 11 and 12 both add actions.
//
// The scroll keys are an **allowlist**, not the viewport's own keymap. That
// keymap binds `d`, `u`, `f`, `b`, `space`, `h` and `l` — and on the dashboard
// `d` is delete, so a user pressing it over the help expects the overlay to
// close, not to half-page down. The diff panel can let every unbound key fall
// through precisely because it has no such contract; here "any key closes" is
// the contract, and only these keys are carved out of it.
var helpScrollKeys = map[string]bool{
	"j": true, "k": true,
	"up": true, "down": true,
	"pgup": true, "pgdown": true,
}

// helpViewportHeight is the lines the overlay body may draw: the same budget
// every list panel gets, since the overlay sits in the same frame.
//
// Before the first WindowSizeMsg the height is genuinely unknown, and it then
// reports the content's own line count — the whole thing, unclipped. Falling
// back to a fixed guess would be worse than useless here: it would make a short
// overlay look scrollable, and j would scroll a screen that fits instead of
// closing it, which is the one thing the footer promises. Same rule
// contentWidth follows in the other axis (view.go).
func helpViewportHeight(height int, body string) int {
	if c := listCapacity(height, 0); c > 0 {
		return c
	}
	return strings.Count(body, "\n") + 1
}

// helpPane builds the overlay's viewport for this frame — content, size and
// scroll position — from the model's single int of state.
//
// **Derived on every render, never stored**, the same move as area 14's window
// and its filter's matching set (DESIGN.md §1). The model keeps only how far
// down the user has scrolled; the content, the height it is measured against and
// the clamp all fall out of the current terminal size. So there is nothing to
// rebuild on a resize, nothing to reset when the overlay is opened on a
// different screen, and an offset can never point past content it was measured
// against — SetYOffset clamps it to whatever this frame actually holds.
func (m Model) helpPane() viewport.Model {
	body, height, _ := m.helpFrame()
	vp := viewport.New(panelViewportWidth(m.styles, m.width), height)
	vp.SetContent(body)
	vp.SetYOffset(m.helpOffset)
	return vp
}

// helpFrame is the overlay's content, the height it may draw into, and how many
// lines it really has — the three numbers every decision here rests on.
func (m Model) helpFrame() (body string, height, total int) {
	body = m.helpBody()
	return body, helpViewportHeight(m.height, body), strings.Count(body, "\n") + 1
}

// helpScrolls reports whether the overlay has more than it can show — which is
// what makes a scroll key mean anything. When everything fits, "any key closes"
// holds unqualified and j/k close it like any other key: one contract, and the
// footer says which one is live, the same way esc does (DESIGN.md §3).
func (m Model) helpScrolls() bool {
	_, height, total := m.helpFrame()
	return total > height
}

// scrollHelp moves the overlay by one of its allowlisted keys, keeping only the
// resulting offset. Driven by explicit calls rather than viewport.Update so no
// binding can arrive with the component.
func (m Model) scrollHelp(key string) Model {
	vp := m.helpPane()
	switch key {
	case "j", "down":
		vp.LineDown(1)
	case "k", "up":
		vp.LineUp(1)
	case "pgdown":
		vp.PageDown()
	case "pgup":
		vp.PageUp()
	}
	m.helpOffset = vp.YOffset
	return m
}

// helpBody is the overlay's content: the key table for the screen it was opened
// from, then the glyph legend.
func (m Model) helpBody() string {
	entries := append(helpEntries(m.activeKeys()), m.unboundEntries()...)
	legend := m.glyphLegend()

	// Measured, not counted: the glyph column holds multi-byte runes, so byte
	// length would misalign every row beneath it. Unbounded, because both halves
	// are generated from the keymap and the glyph legend — this is the one column
	// in the package whose content is not user- or repo-supplied.
	all := append(append([]helpEntry{}, entries...), legend...)
	width := widestCell(len(all), 0, func(i int) string { return all[i].keys })

	lines := []string{
		m.styles.hint.Render("Keys — " + m.screenName()),
		"",
	}
	for _, e := range entries {
		lines = append(lines, fmt.Sprintf("%s  %s",
			m.styles.target.Render(fit(e.keys, width)), m.styles.help.Render(e.what)))
	}

	lines = append(lines, "", m.styles.hint.Render("Glyphs"), "")
	for _, e := range legend {
		// Padded, not re-styled: the glyph arrives already rendered in its own
		// role's color, and wrapping it again would repaint it as one flat color —
		// the bug this replaced.
		lines = append(lines, fmt.Sprintf("%s  %s",
			fit(e.keys, width), m.styles.help.Render(e.what)))
	}

	// Clipped for the same reason every windowed row is: a line wider than the
	// panel wraps to two, and the viewport's height budget is in lines. At the
	// 60-column floor the longest action wording is wider than the panel, so this
	// is not hypothetical.
	for i, l := range lines {
		lines[i] = clipPanelLine(m.styles, m.width, l)
	}
	return strings.Join(lines, "\n")
}

// helpView draws the overlay in the panel's place, over whatever screen asked
// for it.
func (m Model) helpView() string {
	body, height, total := m.helpFrame()
	if total <= height {
		// It fits: draw it straight, so the panel hugs its content exactly as it
		// did before the overlay could scroll. Going through the viewport anyway
		// would pad every short overlay out to the terminal's full height and
		// leave it sitting in a box of blank lines.
		return m.screenView(body, helpLine(m.styles, m.width, nil, []string{"any key closes"}))
	}

	vp := m.helpPane()
	// The footer says what is hidden and in which direction, the same two markers
	// a windowed list carries (`↑ N more` / `↓ N more`) — a clipped edge always
	// says so — and states which key contract is live alongside them.
	lead := []string{}
	if above := vp.YOffset; above > 0 {
		lead = append(lead, fmt.Sprintf("↑ %d more", above))
	}
	if below := total - vp.YOffset - height; below > 0 {
		lead = append(lead, fmt.Sprintf("↓ %d more", below))
	}
	lead = append(lead, "j/k scroll")
	return m.screenView(vp.View(), helpLine(m.styles, m.width, lead, []string{"any other key closes"}))
}
