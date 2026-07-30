package ui

import (
	"strconv"
	"strings"
)

// Action is a named thing a screen can do. It — not the key that triggers it —
// is the stable contract every screen dispatches on. A user-global keymap
// (roadmap area 12) rebinds any action as a pure override layer, so adopting
// named actions from the first screen keeps customization from ever being a
// retrofit. See DESIGN.md §3 and CONTEXT.md (Keybindings).
type Action string

// Actions across every screen. Movement and the two verbs Confirm/Cancel are
// deliberately shared — each screen gives them meaning, so `esc` always means
// "back out" and `enter` always means "commit this screen" wherever the user is.
const (
	ActionMoveUp       Action = "move_up"
	ActionMoveDown     Action = "move_down"
	ActionToggleExpand Action = "toggle_expand"
	ActionAdd          Action = "add"
	ActionDelete       Action = "delete"
	ActionRefresh      Action = "refresh"
	ActionFetch        Action = "fetch"
	ActionLocalOnly    Action = "local_only"
	ActionHelp         Action = "help" // ?: the keys-and-glyphs overlay
	ActionQuit         Action = "quit"

	// Add-flow actions.
	ActionConfirm         Action = "confirm"          // enter: commit the current screen
	ActionCancel          Action = "cancel"           // esc: back out one screen
	ActionToggleCandidate Action = "toggle_candidate" // space: include/exclude a candidate branch
	ActionOpenPicker      Action = "open_picker"      // t: open the target picker for the selection

	// Type-to-filter (area 14), shared by every list long enough to need it.
	// Opening the field is a named action like any other; the keys *inside* it
	// are their own keymap, since almost all of them have to type.
	ActionFilter Action = "filter" // /: narrow the list by substring

	// First-run wizard action: rename the selected target's key inline. Movement,
	// ToggleCandidate (include a ref), Confirm (save), and Cancel (decline) are
	// shared with the pairing checklist, so the wizard needs only this one verb.
	ActionEditKey Action = "edit_key" // e: edit the selected target's key

	// Diff-panel actions (area 5). Scrolling within a file is left to the
	// viewport's own keys (j/k/arrows/pgup/pgdn); these two only step between the
	// colliding files, and Cancel backs out to the dashboard.
	ActionNextFile Action = "next_file" // tab: next colliding file
	ActionPrevFile Action = "prev_file" // shift+tab: previous colliding file

	// Declaring the file unmergeable to git itself (area 5, part 2): w opens the
	// two-step overlay over the diff panel, which then reuses the shared
	// move/confirm/cancel verbs like every other picker.
	ActionDeclare Action = "declare" // w: declare this file's pattern -merge

	// Local-only manager actions (area 6). Refresh and the shared
	// move/confirm/cancel verbs carry over from the dashboard; these three are
	// the screen's own. Holding is deliberately *not* ActionAdd reused: the help
	// table is generated per action, so one action reused across two screens
	// would have to describe itself as both "add a ticket" and "hold a change".
	ActionHoldLocal Action = "hold_local" // a: hold a working-tree change locally
	ActionRelease   Action = "release"    // d: stop holding the selected path
	ActionEditNote  Action = "edit_note"  // n: annotate why a path is held

	// The one-key shelve sequence (area 7): stash → pull the target → merge →
	// pop, on the checked-out branch. Its own verb rather than Confirm reused —
	// the help table is generated per action, so a name has to mean one thing.
	ActionShelve Action = "shelve" // s: run the sequence on the selected branch

	// The update sequence (area 17): the same merge carried all the way — the
	// branch checked out, its own upstream pulled, the result pushed, and the
	// user returned to where they were standing. Two verbs rather than one
	// because they differ by *commitment*: `s` publishes nothing and stays on the
	// checked-out branch, `u` publishes and works on any paired branch. The cost
	// is that their help entries have to carry the distinction on their own, since
	// the table is generated per action.
	ActionUpdate Action = "update" // u: bring the selected branch up to date and publish it

	// The targets screen (19e): every configured target and the ref behind it.
	// Its own verb rather than ActionLocalOnly's shape reused, for the reason
	// every other screen-opening action has one — the help table is generated
	// per action, so a name has to describe exactly one thing.
	ActionTargets Action = "targets" // t: show the targets and the refs they point at

	// Re-pointing a target at a different ref (19e), from the targets screen. Its
	// own verb rather than the wizard's ActionEditKey reused: that one renames a
	// key, this one changes the ref behind it, and the help table is generated per
	// action so a name has to mean exactly one thing. They share the `e` key on
	// their respective screens because both edit the selected row.
	ActionRepoint Action = "repoint" // e: point the selected target at another ref
)

// pickTargetPrefix builds the parametric "assign the Nth target" accelerator
// actions (DESIGN.md §3). Keeping them a family rather than nine constants lets
// a rebind map any key to "pick the Nth target" without a code change, while
// dispatch recovers N from the action string.
const pickTargetPrefix = "pick_target_"

// ActionPickTarget names the accelerator that assigns the n-th configured
// target (1-based, matching the number keys) to the selected candidate.
func ActionPickTarget(n int) Action {
	return Action(pickTargetPrefix + strconv.Itoa(n))
}

// pickTargetIndex recovers the 0-based target index from a pick-target action,
// reporting false for any other action.
func pickTargetIndex(a Action) (int, bool) {
	rest, ok := strings.CutPrefix(string(a), pickTargetPrefix)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 1 {
		return 0, false
	}
	return n - 1, true
}

// Keymap resolves a pressed key (as Bubble Tea's KeyMsg.String()) to an action.
// It is a plain map so a later override layer is a merge, not a code change.
type Keymap map[string]Action

// keymaps holds one default binding per screen. Each is independently
// overridable, so area 12 rebinds the add flow and the dashboard separately
// without either leaking into the other.
type keymaps struct {
	dashboard      Keymap
	addID          Keymap
	pairing        Keymap
	picker         Keymap
	confirmDelete  Keymap
	wizard         Keymap
	diff           Keymap
	declare        Keymap
	localOnly      Keymap
	localAdd       Keymap
	localNote      Keymap
	shelve         Keymap
	confirmStash   Keymap
	filter         Keymap
	targets        Keymap
	repoint        Keymap
	confirmRepoint Keymap
}

func defaultKeymaps() keymaps {
	return keymaps{
		dashboard:      DefaultDashboardKeys(),
		addID:          DefaultAddIDKeys(),
		pairing:        DefaultPairingKeys(),
		picker:         DefaultPickerKeys(),
		confirmDelete:  DefaultConfirmDeleteKeys(),
		wizard:         DefaultWizardKeys(),
		diff:           DefaultDiffKeys(),
		declare:        DefaultDeclareKeys(),
		localOnly:      DefaultLocalOnlyKeys(),
		localAdd:       DefaultLocalAddKeys(),
		localNote:      DefaultLocalNoteKeys(),
		shelve:         DefaultShelveKeys(),
		confirmStash:   DefaultConfirmStashKeys(),
		filter:         DefaultFilterKeys(),
		targets:        DefaultTargetsKeys(),
		repoint:        DefaultRepointKeys(),
		confirmRepoint: DefaultConfirmRepointKeys(),
	}
}

// DefaultTargetsKeys binds the targets screen (19e): movement, `e` to point the
// selected target at a different ref, and the ways out.
//
// enter stays deliberately unbound rather than made a synonym for `e` or for
// esc. A key that means "commit this screen" everywhere else must not quietly
// mean "edit the selected row" here — that is what `e` means on the first-run
// wizard, and the two screens now read the same way.
func DefaultTargetsKeys() Keymap {
	return Keymap{
		"j":      ActionMoveDown,
		"down":   ActionMoveDown,
		"k":      ActionMoveUp,
		"up":     ActionMoveUp,
		"e":      ActionRepoint,
		"esc":    ActionCancel,
		"?":      ActionHelp,
		"q":      ActionQuit,
		"ctrl+c": ActionQuit,
	}
}

// DefaultRepointKeys binds the ref picker opened by `e` — the same
// move/enter/esc shape as the target picker and the declare overlay, plus `/`,
// since it offers every ref under refs/remotes and that is the one list area 14
// found filtering load-bearing on.
//
// No `?`, the same as the target picker, the declare overlay and the hold
// picker: a momentary choice step carries its own one-line help (DESIGN.md §2).
// `q` is left unbound too — while a choice is open, backing out of it is esc.
func DefaultRepointKeys() Keymap {
	return Keymap{
		"j":      ActionMoveDown,
		"down":   ActionMoveDown,
		"k":      ActionMoveUp,
		"up":     ActionMoveUp,
		"/":      ActionFilter,
		"enter":  ActionConfirm,
		"esc":    ActionCancel,
		"ctrl+c": ActionQuit,
	}
}

// DefaultConfirmRepointKeys binds the re-point confirmation — the y/n question
// between picking a ref and config.json being rewritten.
//
// The delete confirmation's shape rather than a picker's, because it is a yes/no
// and not a list, and `?` is bound for the reason it is bound on the stash plan
// (area 17b): while a confirmation is on screen, the key that opens the help must
// not also be the key that commits the write. The `?` overlay's "any key closes,
// and is consumed" rule is what makes that hold.
func DefaultConfirmRepointKeys() Keymap {
	return Keymap{
		"y":      ActionConfirm,
		"enter":  ActionConfirm,
		"n":      ActionCancel,
		"esc":    ActionCancel,
		"?":      ActionHelp,
		"ctrl+c": ActionQuit,
	}
}

// DefaultFilterKeys binds the filter field while it has focus, on whichever list
// screen opened it. Only the control keys are bound — every other key has to
// type, or an incremental filter is impossible on a screen whose verbs are
// single letters (`e`, `t`, `space`, `1`–`9`).
//
// Movement stays bound so the user can arrow straight onto a match without
// leaving the field, and it is deliberately the *arrows* rather than j/k: j and k
// are two of the letters that must remain typeable.
//
// enter and esc split the two ways out. enter accepts the query and hands the
// keys back to the list, so j/k navigate what is left; esc clears the filter
// outright, which is the screen's usual "back out one step" — the step being
// backed out of is the narrowing, not the screen.
func DefaultFilterKeys() Keymap {
	return Keymap{
		"up":     ActionMoveUp,
		"down":   ActionMoveDown,
		"enter":  ActionConfirm,
		"esc":    ActionCancel,
		"ctrl+c": ActionQuit,
	}
}

// DefaultConfirmStashKeys binds the stash-plan overlay (area 17b) — the one
// moment `u` asks before it acts, when it has to leave a branch that has
// uncommitted work on it.
//
// Deliberately the delete confirmation's shape rather than the picker overlays':
// this is a y/n question, not a list, so it binds the keys that answer one. `q`
// is left unbound for the reason the delete confirm leaves it unbound — while a
// yes/no is on screen the contract is yes or no, and a key that quietly means a
// third thing is not part of it. ctrl+c still quits, as everywhere.
func DefaultConfirmStashKeys() Keymap {
	return Keymap{
		"y":      ActionConfirm,
		"enter":  ActionConfirm,
		"n":      ActionCancel,
		"esc":    ActionCancel,
		"?":      ActionHelp,
		"ctrl+c": ActionQuit,
	}
}

// DefaultShelveKeys binds the shelve report (area 7). It is a report, not a list:
// there is nothing to move over and nothing to choose, so esc is the only verb —
// and while the mutating steps run it is refused rather than obeyed, since there
// is no cancelling into an undefined middle.
func DefaultShelveKeys() Keymap {
	return Keymap{
		"esc":    ActionCancel,
		"?":      ActionHelp,
		"q":      ActionQuit,
		"ctrl+c": ActionQuit,
	}
}

// DefaultDashboardKeys is the considered-but-not-sacred default binding for the
// dashboard, mirroring the table in DESIGN.md §3. Keys absent here fall through
// to no action; an override that leaves an action unbound keeps this default.
func DefaultDashboardKeys() Keymap {
	return Keymap{
		"j":     ActionMoveDown,
		"down":  ActionMoveDown,
		"k":     ActionMoveUp,
		"up":    ActionMoveUp,
		"enter": ActionToggleExpand,
		" ":     ActionToggleExpand,
		"a":     ActionAdd,
		"d":     ActionDelete,
		"r":     ActionRefresh,
		"f":     ActionFetch,
		"esc":   ActionCancel, // aborts an in-flight fetch; a no-op otherwise
		"l":     ActionLocalOnly,
		"s":     ActionShelve,
		"u":     ActionUpdate,
		"t":     ActionTargets, // the pairing checklist's own t opens a target picker

		"?":      ActionHelp,
		"q":      ActionQuit,
		"ctrl+c": ActionQuit,
	}
}

// DefaultAddIDKeys binds only the control keys on the ID-entry screen; every
// other key is left to the text input so typing lands in the field.
func DefaultAddIDKeys() Keymap {
	return Keymap{
		"enter":  ActionConfirm,
		"esc":    ActionCancel,
		"ctrl+c": ActionQuit,
	}
}

// DefaultPairingKeys binds the pairing checklist (DESIGN.md §3), including the
// 1–9 accelerators that assign a target without opening the picker.
func DefaultPairingKeys() Keymap {
	k := Keymap{
		"j":      ActionMoveDown,
		"down":   ActionMoveDown,
		"k":      ActionMoveUp,
		"up":     ActionMoveUp,
		" ":      ActionToggleCandidate,
		"t":      ActionOpenPicker,
		"/":      ActionFilter,
		"enter":  ActionConfirm,
		"esc":    ActionCancel,
		"?":      ActionHelp,
		"ctrl+c": ActionQuit,
	}
	for n := 1; n <= 9; n++ {
		k[strconv.Itoa(n)] = ActionPickTarget(n)
	}
	return k
}

// DefaultPickerKeys binds the target picker overlay — the same move/confirm/
// cancel shape as the dashboard, so the overlay needs no learning.
func DefaultPickerKeys() Keymap {
	return Keymap{
		"j":      ActionMoveDown,
		"down":   ActionMoveDown,
		"k":      ActionMoveUp,
		"up":     ActionMoveUp,
		"enter":  ActionConfirm,
		"esc":    ActionCancel,
		"ctrl+c": ActionQuit,
	}
}

// DefaultConfirmDeleteKeys binds the delete confirmation: y/enter commit, n/esc
// back out. Deleting a ticket drops its pairings, so it is never one keystroke.
func DefaultConfirmDeleteKeys() Keymap {
	return Keymap{
		"y":      ActionConfirm,
		"enter":  ActionConfirm,
		"n":      ActionCancel,
		"esc":    ActionCancel,
		"?":      ActionHelp,
		"ctrl+c": ActionQuit,
	}
}

// DefaultWizardKeys binds the first-run wizard: a checklist of remote refs with
// space to include one as a target, e to rename its key, / to narrow the list,
// enter to save, esc to decline (back to the hand-edit fallback). Deliberately
// the same move/space//-filter/enter/esc shape as the pairing checklist, so the
// wizard needs no learning.
func DefaultWizardKeys() Keymap {
	return Keymap{
		"j":      ActionMoveDown,
		"down":   ActionMoveDown,
		"k":      ActionMoveUp,
		"up":     ActionMoveUp,
		" ":      ActionToggleCandidate,
		"e":      ActionEditKey,
		"/":      ActionFilter,
		"enter":  ActionConfirm,
		"esc":    ActionCancel,
		"ctrl+c": ActionQuit,
	}
}

// DefaultDiffKeys binds the diff panel: tab/shift+tab step between the colliding
// files, esc backs out to the dashboard. Every other key — j/k, arrows, pgup/
// pgdn — is left unbound so it falls through to the viewport's own scrolling,
// which is why the panel needs no bespoke scroll bindings here.
func DefaultDiffKeys() Keymap {
	return Keymap{
		"tab":       ActionNextFile,
		"shift+tab": ActionPrevFile,
		"w":         ActionDeclare,
		"?":         ActionHelp,
		"esc":       ActionCancel,
		"q":         ActionQuit,
		"ctrl+c":    ActionQuit,
	}
}

// DefaultDeclareKeys binds the declare overlay — pick a pattern, then pick where
// it is written. Deliberately the same move/enter/esc shape as the target picker
// overlay it sits beside, so an overlay is an overlay wherever the user meets
// one. Every key is bound here: unlike the panel underneath, nothing falls
// through to the viewport while a choice is open.
func DefaultDeclareKeys() Keymap {
	return Keymap{
		"j":      ActionMoveDown,
		"down":   ActionMoveDown,
		"k":      ActionMoveUp,
		"up":     ActionMoveUp,
		"enter":  ActionConfirm,
		"esc":    ActionCancel,
		"ctrl+c": ActionQuit,
	}
}

// DefaultLocalOnlyKeys binds the local-only manager (area 6). `a` holds a
// change and `r` refreshes, both carrying their dashboard meaning across; `d`
// releases, taking the dashboard's "remove the selected thing" key rather than
// the more mnemonic `r`, which would mean a reflexive refresh could silently
// release a hold instead.
//
// Release needs no y/n confirm — unlike deleting a ticket it destroys nothing,
// since a released file's edits reappear as ordinary working-tree changes.
func DefaultLocalOnlyKeys() Keymap {
	return Keymap{
		"j":      ActionMoveDown,
		"down":   ActionMoveDown,
		"k":      ActionMoveUp,
		"up":     ActionMoveUp,
		"a":      ActionHoldLocal,
		"d":      ActionRelease,
		"n":      ActionEditNote,
		"r":      ActionRefresh,
		"esc":    ActionCancel,
		"?":      ActionHelp,
		"q":      ActionQuit,
		"ctrl+c": ActionQuit,
	}
}

// DefaultLocalAddKeys binds the candidate picker — the same move/enter/esc
// shape as the target picker and the declare overlay, so an overlay is an
// overlay wherever the user meets one.
func DefaultLocalAddKeys() Keymap {
	return Keymap{
		"j":      ActionMoveDown,
		"down":   ActionMoveDown,
		"k":      ActionMoveUp,
		"up":     ActionMoveUp,
		"enter":  ActionConfirm,
		"esc":    ActionCancel,
		"ctrl+c": ActionQuit,
	}
}

// DefaultLocalNoteKeys binds only the control keys of the note editor; every
// other key is left to the text input so typing lands in the field, the same
// split the ID-entry screen uses.
func DefaultLocalNoteKeys() Keymap {
	return Keymap{
		"enter":  ActionConfirm,
		"esc":    ActionCancel,
		"ctrl+c": ActionQuit,
	}
}

// action looks up the action bound to a key, if any.
func (k Keymap) action(key string) (Action, bool) {
	a, ok := k[key]
	return a, ok
}
