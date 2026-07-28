package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
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
	ActionShelve:          "stash, pull the target, merge it in, put your work back",
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
	ActionShelve,
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

// helpView draws the overlay in the panel's place, over whatever screen asked
// for it.
func (m Model) helpView() string {
	entries := append(helpEntries(m.activeKeys()), m.unboundEntries()...)
	legend := m.glyphLegend()

	// Measured, not counted: the glyph column holds multi-byte runes, so byte
	// length would misalign every row beneath it.
	width := 0
	for _, e := range append(append([]helpEntry{}, entries...), legend...) {
		if w := lipgloss.Width(e.keys); w > width {
			width = w
		}
	}

	lines := []string{
		m.styles.hint.Render("Keys — " + m.screenName()),
		"",
	}
	for _, e := range entries {
		lines = append(lines, fmt.Sprintf("%s  %s",
			m.styles.target.Render(padCell(e.keys, width)), m.styles.help.Render(e.what)))
	}

	lines = append(lines, "", m.styles.hint.Render("Glyphs"), "")
	for _, e := range legend {
		// Padded, not re-styled: the glyph arrives already rendered in its own
		// role's color, and wrapping it again would repaint it as one flat color —
		// the bug this replaced.
		lines = append(lines, fmt.Sprintf("%s  %s",
			padCell(e.keys, width), m.styles.help.Render(e.what)))
	}

	return m.screenView(strings.Join(lines, "\n"), m.styles.help.Render("any key closes"))
}

// padCell pads to a display width rather than a byte count, so a column of
// glyphs lines up with a column of ASCII key names.
func padCell(s string, w int) string {
	if pad := w - lipgloss.Width(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}
