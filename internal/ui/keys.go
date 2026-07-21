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
	ActionQuit         Action = "quit"

	// Add-flow actions.
	ActionConfirm         Action = "confirm"          // enter: commit the current screen
	ActionCancel          Action = "cancel"           // esc: back out one screen
	ActionToggleCandidate Action = "toggle_candidate" // space: include/exclude a candidate branch
	ActionOpenPicker      Action = "open_picker"      // t: open the target picker for the selection

	// First-run wizard action: rename the selected target's key inline. Movement,
	// ToggleCandidate (include a ref), Confirm (save), and Cancel (decline) are
	// shared with the pairing checklist, so the wizard needs only this one verb.
	ActionEditKey Action = "edit_key" // e: edit the selected target's key
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
	dashboard     Keymap
	addID         Keymap
	pairing       Keymap
	picker        Keymap
	confirmDelete Keymap
	wizard        Keymap
}

func defaultKeymaps() keymaps {
	return keymaps{
		dashboard:     DefaultDashboardKeys(),
		addID:         DefaultAddIDKeys(),
		pairing:       DefaultPairingKeys(),
		picker:        DefaultPickerKeys(),
		confirmDelete: DefaultConfirmDeleteKeys(),
		wizard:        DefaultWizardKeys(),
	}
}

// DefaultDashboardKeys is the considered-but-not-sacred default binding for the
// dashboard, mirroring the table in DESIGN.md §3. Keys absent here fall through
// to no action; an override that leaves an action unbound keeps this default.
func DefaultDashboardKeys() Keymap {
	return Keymap{
		"j":      ActionMoveDown,
		"down":   ActionMoveDown,
		"k":      ActionMoveUp,
		"up":     ActionMoveUp,
		"enter":  ActionToggleExpand,
		" ":      ActionToggleExpand,
		"a":      ActionAdd,
		"d":      ActionDelete,
		"r":      ActionRefresh,
		"f":      ActionFetch,
		"esc":    ActionCancel, // aborts an in-flight fetch; a no-op otherwise
		"l":      ActionLocalOnly,
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
		"enter":  ActionConfirm,
		"esc":    ActionCancel,
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
		"ctrl+c": ActionQuit,
	}
}

// DefaultWizardKeys binds the first-run wizard: a checklist of remote refs with
// space to include one as a target, e to rename its key, enter to save, esc to
// decline (back to the hand-edit fallback). Deliberately the same move/space/
// enter/esc shape as the pairing checklist, so the wizard needs no learning.
func DefaultWizardKeys() Keymap {
	return Keymap{
		"j":      ActionMoveDown,
		"down":   ActionMoveDown,
		"k":      ActionMoveUp,
		"up":     ActionMoveUp,
		" ":      ActionToggleCandidate,
		"e":      ActionEditKey,
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
