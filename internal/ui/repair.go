package ui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
)

// Re-pairing a branch (roadmap 19b): `p` on a dashboard branch row opens the
// target picker over it and writes the pick.
//
// It exists because TargetKey was write-once. It is set in exactly one place —
// savePairing, inside the add flow — so a branch paired to the wrong target was
// visible on the dashboard and correctable nowhere: `d` deletes the whole ticket
// (re-pairing every branch on it to fix one), re-adding the same ID is refused as
// already tracked, and the only other route was hand-editing state.json with
// Drift closed.
//
// 19e is the same shape about the other field, and the pair of them is the
// distinction worth keeping straight. A *target's* `Ref` is what a key points at
// and lives in config.json; a *branch's* `TargetKey` is which target it aims for
// and lives in state.json. The incident that raised area 19 was the first; this is
// the second, and each is uncorrectable without the other's screen.
//
// Unlike the re-point, the pick commits on `enter` with no confirmation — see
// commitRepair for the argument, which is DESIGN.md §3's own.

// repairState is the open re-pair: the target picker over the dashboard, and the
// branch it is about.
//
// The branch is held by ticket ID and name rather than as a rowRef, for the
// reason repointState holds a key rather than an index: a row index means "the
// n-th visible row", which a collapse or a landed sweep can quietly redefine
// under an open overlay. A name resolves to the same branch or to none, and none
// is a case commitRepair already has to state.
type repairState struct {
	open     bool
	ticketID string
	branch   string
	from     string // the target key it is paired to now: the picker marks nothing else
	cursor   int    // index into cfg.Targets
}

// beginRepair opens the picker for the selected branch row.
//
// No Cmd: the targets are already in the model, the same as the targets screen —
// unlike the re-point's ref picker, there is nothing to ask git. Which targets
// exist is a question the config answers, and a config Drift has loaded is the
// config in force.
func (m Model) beginRepair() (tea.Model, tea.Cmd) {
	row, ok := m.selectedRow()
	if !ok || !row.isBranch() {
		m.notice = "select a branch row — a pairing belongs to a branch, not a ticket"
		return m, nil
	}
	if len(m.cfg.Targets) == 0 {
		// Reachable only from a hand-edited config: SaveConfig's validate() refuses
		// to write a target-less one, and every row on screen would already be
		// flagged "⚠ unknown target". Say what is missing rather than opening a
		// picker with nothing in it.
		m.notice = "no targets configured — " + m.paths.Config + " names none"
		return m, nil
	}

	t := m.store.Tickets[row.ticket]
	br := t.Branches[row.branch]
	m.repair = repairState{
		open:     true,
		ticketID: t.ID,
		branch:   br.Branch,
		from:     br.TargetKey,
		cursor:   m.targetIndex(br.TargetKey),
	}
	m.notice = ""
	return m, nil
}

// targetIndex is the picker's opening cursor: the row's current target if it has
// a configured one, else the top of the list. The add flow's picker opens the
// same way (selectedTargetIndex) — a picker that opens anywhere but on the value
// it is changing makes the user find that value before they can decide against
// changing it.
func (m Model) targetIndex(key string) int {
	for i, t := range m.cfg.Targets {
		if t.Key == key {
			return i
		}
	}
	return 0
}

// dispatchRepair runs one action in the picker. It shadows the dashboard's keymap
// while it is open, exactly as the declare overlay shadows the diff panel's.
func (m Model) dispatchRepair(action Action) (tea.Model, tea.Cmd) {
	switch action {
	case ActionCancel:
		m.repair = repairState{}
		return m, nil

	case ActionMoveUp:
		if m.repair.cursor > 0 {
			m.repair.cursor--
		}
		return m, nil

	case ActionMoveDown:
		if m.repair.cursor < len(m.cfg.Targets)-1 {
			m.repair.cursor++
		}
		return m, nil

	case ActionConfirm:
		return m.commitRepair(m.repair.cursor)
	}

	// The 1–9 accelerators, live here because the body draws them (DefaultPickerKeys).
	if idx, ok := pickTargetIndex(action); ok {
		return m.commitRepair(idx)
	}
	return m, nil
}

// commitRepair writes the pick and re-reads the branch's standing against it.
//
// **It commits on enter, with no y/n**, and that is an argument rather than an
// omission. DESIGN.md §3 names the re-point confirmation as *the one place* a
// picker in Drift does not, and the reason it earned one was reach: re-pointing a
// target silently re-bases every paired branch's ↓behind onto a different ref,
// which is the one effect no row can show as it happens. Re-pairing one branch
// re-bases one row, and that row shows its new target key the moment this
// returns. A confirmation here would be a keypress spent on something already
// visible.
//
// The model is updated before the write, which is the opposite of applyRepoint's
// rule and deliberately so. That rule is about config.json: a model holding a ref
// the file does not makes the very next sweep report correct-looking numbers
// about a target that is not on disk. This is a pairing in state.json — the file
// savePairing, doDelete and the note editor all write optimistically — and the
// sweep cannot measure the new pairing at all until it is in the model. A failed
// write says so on the status line (saveStateMsg), where the numbers it would
// have produced stay honest either way.
func (m Model) commitRepair(idx int) (tea.Model, tea.Cmd) {
	r := m.repair
	if idx < 0 || idx >= len(m.cfg.Targets) {
		// An accelerator for a slot no target fills. The picker stays open: the user
		// pressed a digit meaning "that one", and closing on it would read as having
		// chosen something.
		m.notice = "no target in that slot"
		return m, nil
	}
	key := m.cfg.Targets[idx].Key

	m.repair = repairState{}
	if key == r.from {
		// Said out loud rather than written, the rule the re-point landed on: it is
		// the one outcome where "it worked" and "nothing happened" leave the row
		// identical, so the screen has to be what tells them apart.
		m.notice = r.branch + " is already paired to " + key
		return m, nil
	}

	st, ok := m.store.SetBranchTarget(r.ticketID, r.branch, key)
	if !ok {
		// The selection went stale — only reachable if the store changed under the
		// overlay. Say so rather than write a pairing onto something that is gone.
		m.notice = "no branch " + quote(r.branch) + " on " + r.ticketID + " any more — nothing was changed"
		return m, nil
	}
	m.store = st

	// No success notice: the row behind the overlay now shows the new target key,
	// which is permanent where a notice is transient — and the sweep this starts
	// clears the status line on arrival anyway (applyStatus), so a notice would be
	// wiped by the very work that proves the pairing took.
	//
	// The sweep is local, not a fetch, for the reason applyRepoint's is: nothing
	// about which target a branch aims at changes what is under refs/remotes, so
	// there is nothing a fetch would make resolvable. How fresh the target is
	// remains `f`'s question.
	m, id := m.supersedeSweeps()
	m.loading = true
	m.notice = ""
	return m, tea.Batch(
		m.spin.Tick,
		saveStateCmd(m.repo, m.store),
		loadStatusCmd(context.Background(), m.repo, m.cfg, m.store.Tickets, id),
	)
}
