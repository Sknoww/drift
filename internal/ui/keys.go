package ui

// Action is a named thing a screen can do. It — not the key that triggers it —
// is the stable contract every screen dispatches on. A user-global keymap
// (roadmap area 12) rebinds any action as a pure override layer, so adopting
// named actions from the first screen keeps customization from ever being a
// retrofit. See DESIGN.md §3 and CONTEXT.md (Keybindings).
type Action string

// Dashboard actions. Add-flow and local-only actions arrive with their screens.
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
)

// Keymap resolves a pressed key (as Bubble Tea's KeyMsg.String()) to an action.
// It is a plain map so a later override layer is a merge, not a code change.
type Keymap map[string]Action

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
		"l":      ActionLocalOnly,
		"q":      ActionQuit,
		"ctrl+c": ActionQuit,
	}
}

// action looks up the action bound to a key, if any.
func (k Keymap) action(key string) (Action, bool) {
	a, ok := k[key]
	return a, ok
}
