package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Sknoww/drift/internal/store"
)

// addFlow is the state of the add-ticket flow: the entered ID, the candidate
// branches Git matched to it, and the pairing choices in progress. It is only
// meaningful while the model is on screenPairing.
type addFlow struct {
	id         string
	candidates []candidate
	cursor     int  // index into visible(), not into candidates
	loaded     bool // false until CandidateBranches has answered

	filter filterState // type-to-filter over the candidates (area 14)

	picker    bool // the target picker overlay is open over the checklist
	pickerCur int  // cursor into cfg.Targets while the picker is open
}

// visible reports the indices of the candidates surviving the filter, in list
// order — the same derived-not-stored shape as the wizard's (filter.go).
//
// CandidateBranches already narrows to local branches containing the ticket ID,
// so this list is two or three rows on the repo shape that makes the wizard
// unusable. The filter is here for consistency of shape rather than because the
// screen is drowning: one list screen that filters and one that does not is a
// worse tool than either.
func (a addFlow) visible() []int {
	return filterVisible(len(a.candidates), func(i int) bool {
		return a.filter.matches(a.candidates[i].branch)
	})
}

// selected resolves the cursor to an index into candidates, reporting false when
// the query matches nothing.
func (a addFlow) selected() (int, bool) {
	vis := a.visible()
	if a.cursor < 0 || a.cursor >= len(vis) {
		return 0, false
	}
	return vis[a.cursor], true
}

// applyFilter folds a changed query back in, keeping the cursor on the row it
// was on when that row survives the change.
func (a addFlow) applyFilter(f filterState) addFlow {
	idx, ok := a.selected()
	a.filter = f
	if !ok {
		a.cursor = 0
		return a
	}
	a.cursor = cursorFor(a.visible(), idx)
	return a
}

// reveal drops the filter and puts the cursor on candidates[idx], so a save
// blocked by a hidden branch names a row the user can actually see.
func (a addFlow) reveal(idx int) addFlow {
	a.filter = a.filter.clear()
	a.cursor = cursorFor(a.visible(), idx)
	return a
}

// candidate is one local branch offered for pairing. included says the user
// wants it on the ticket; targetKey is the target they assigned. An included
// candidate with no targetKey is the error state DESIGN.md §2 calls out — the
// software never guesses a target, so save is blocked until it is resolved.
type candidate struct {
	branch    string
	included  bool
	targetKey string
}

// beginAdd opens the ID-entry screen with a fresh, focused text input.
func (m Model) beginAdd() (tea.Model, tea.Cmd) {
	ti := textinput.New()
	ti.Placeholder = "TICKET-123"
	ti.CharLimit = 64
	ti.Focus()

	m.input = ti
	m.screen = screenAddID
	m.notice = ""
	return m, textinput.Blink
}

// dispatchAddID handles the ID-entry screen. Only Confirm/Cancel/Quit reach
// here; ordinary keystrokes are fed to the text input by Update.
func (m Model) dispatchAddID(action Action) (tea.Model, tea.Cmd) {
	switch action {
	case ActionCancel:
		return m.cancelToDashboard(), nil

	case ActionConfirm:
		id := strings.TrimSpace(m.input.Value())
		if id == "" {
			m.notice = "enter a ticket ID"
			return m, nil
		}
		if _, exists := m.store.Ticket(id); exists {
			m.notice = fmt.Sprintf("%s is already tracked", id)
			return m, nil
		}
		// Move to pairing and scan for candidate branches asynchronously, so
		// the UI never blocks on the branch listing.
		m.add = addFlow{id: id}
		m.screen = screenPairing
		m.notice = ""
		return m, loadCandidatesCmd(m.repo, id)
	}
	return m, nil
}

// applyCandidates folds a completed candidate scan into the add flow. A scan
// that lands after the user has cancelled or started a different add is stale
// and dropped.
func (m Model) applyCandidates(msg candidatesMsg) Model {
	if m.screen != screenPairing || msg.id != m.add.id {
		return m
	}
	m.add.loaded = true
	if msg.err != nil {
		m.notice = "couldn't list candidate branches: " + msg.err.Error()
		return m
	}
	cands := make([]candidate, len(msg.branches))
	for i, b := range msg.branches {
		cands[i] = candidate{branch: b}
	}
	m.add.candidates = cands
	m.add.cursor = 0
	return m
}

// dispatchPairing handles the checklist, delegating to the picker while its
// overlay is open.
func (m Model) dispatchPairing(action Action) (tea.Model, tea.Cmd) {
	if m.add.picker {
		return m.dispatchPicker(action)
	}
	if m.add.filter.open {
		return m.dispatchPairingFilter(action)
	}

	switch action {
	case ActionCancel:
		// A filter left applied is a step to back out of, the same as the picker
		// overlay above: enter accepts a query and closes the field but keeps the
		// narrowing, so the next esc undoes that rather than throwing away the add.
		if m.add.filter.active() {
			m.add = m.add.applyFilter(m.add.filter.clear())
			return m, nil
		}
		return m.cancelToDashboard(), nil

	case ActionMoveUp:
		if m.add.cursor > 0 {
			m.add.cursor--
		}
		return m, nil

	case ActionMoveDown:
		if m.add.cursor < len(m.add.visible())-1 {
			m.add.cursor++
		}
		return m, nil

	case ActionToggleCandidate:
		return m.toggleCandidate(), nil

	case ActionFilter:
		var cmd tea.Cmd
		m.add.filter, cmd = m.add.filter.begin()
		return m, cmd

	case ActionOpenPicker:
		if _, ok := m.add.selected(); !ok {
			return m, nil
		}
		m.add.picker = true
		m.add.pickerCur = m.selectedTargetIndex()
		return m, nil

	case ActionConfirm:
		return m.savePairing()
	}

	// The 1–9 accelerators assign the Nth target directly, no picker.
	if idx, ok := pickTargetIndex(action); ok {
		return m.assignTarget(idx), nil
	}
	return m, nil
}

// dispatchPairingFilter runs the control keys of the filter field while it has
// focus. Only these four actions are reachable — the filter keymap binds nothing
// else, so `space`, `t` and the 1–9 accelerators all type into the query instead
// of acting on the list underneath.
func (m Model) dispatchPairingFilter(action Action) (tea.Model, tea.Cmd) {
	switch action {
	case ActionConfirm:
		m.add.filter = m.add.filter.commit() // keep the query, hand j/k back
	case ActionCancel:
		m.add = m.add.applyFilter(m.add.filter.clear())
	case ActionMoveUp:
		if m.add.cursor > 0 {
			m.add.cursor--
		}
	case ActionMoveDown:
		if m.add.cursor < len(m.add.visible())-1 {
			m.add.cursor++
		}
	}
	return m, nil
}

// dispatchPicker handles the target picker overlay.
func (m Model) dispatchPicker(action Action) (tea.Model, tea.Cmd) {
	switch action {
	case ActionCancel:
		m.add.picker = false // back to the checklist, no assignment
		return m, nil

	case ActionMoveUp:
		if m.add.pickerCur > 0 {
			m.add.pickerCur--
		}
		return m, nil

	case ActionMoveDown:
		if m.add.pickerCur < len(m.cfg.Targets)-1 {
			m.add.pickerCur++
		}
		return m, nil

	case ActionConfirm:
		m = m.assignTarget(m.add.pickerCur)
		m.add.picker = false
		return m, nil
	}
	return m, nil
}

// toggleCandidate includes or excludes the selected candidate. Excluding it
// also clears any target, so a re-included branch starts unassigned rather than
// silently keeping a stale pairing.
func (m Model) toggleCandidate() Model {
	idx, ok := m.add.selected()
	if !ok {
		return m
	}
	c := m.add.candidates[idx]
	c.included = !c.included
	if !c.included {
		c.targetKey = ""
	}
	m.add.candidates[idx] = c
	return m
}

// assignTarget assigns the idx-th configured target to the selected candidate,
// which also includes it. An index past the configured targets is a no-op with
// a hint — the accelerators cover only as many targets as exist.
func (m Model) assignTarget(idx int) Model {
	if idx < 0 || idx >= len(m.cfg.Targets) {
		m.notice = "no target in that slot"
		return m
	}
	ci, ok := m.add.selected()
	if !ok {
		return m
	}
	c := m.add.candidates[ci]
	c.included = true
	c.targetKey = m.cfg.Targets[idx].Key
	m.add.candidates[ci] = c
	m.notice = ""
	return m
}

// selectedTargetIndex is the picker's opening cursor: the selected candidate's
// current target if it has one, else the top of the list.
func (m Model) selectedTargetIndex() int {
	ci, ok := m.add.selected()
	if !ok {
		return 0
	}
	key := m.add.candidates[ci].targetKey
	for i, t := range m.cfg.Targets {
		if t.Key == key {
			return i
		}
	}
	return 0
}

// savePairing builds the ticket from the included candidates and persists it.
// An included-but-unassigned branch blocks the save — the same "never guess"
// rule as pairing. Saving with nothing included is allowed: a bare ticket is a
// valid state (branches paired later).
//
// It walks every candidate, not just the visible ones: a branch included and
// then filtered out is still included (DESIGN.md §1). So a block can land on a
// row the query is hiding, and it reveals that row before reporting it.
func (m Model) savePairing() (tea.Model, tea.Cmd) {
	var branches []store.TicketBranch
	for i, c := range m.add.candidates {
		if !c.included {
			continue
		}
		if c.targetKey == "" {
			m.notice = "assign a target to " + c.branch + " (t or 1–9) before saving"
			m.add = m.add.reveal(i)
			return m, nil
		}
		branches = append(branches, store.TicketBranch{Branch: c.branch, TargetKey: c.targetKey})
	}

	ticket := store.Ticket{ID: m.add.id, Branches: branches}
	m.store.Tickets = append(m.store.Tickets, ticket)
	m.expanded[ticket.ID] = len(branches) > 0
	// Select the new ticket's headline row. With branch rows now interleaved, its
	// index is no longer len(Tickets)-1, so resolve it through the visible list.
	m.cursor = m.ticketRowIndex(len(m.store.Tickets) - 1)

	m.screen = screenDashboard
	m.add = addFlow{}
	m.notice = "added " + ticket.ID

	// Persist, and sweep so the new ticket's rows get their ahead/behind.
	m, id := m.supersedeSweeps()
	m.loading = true
	return m, tea.Batch(
		m.spin.Tick,
		saveStateCmd(m.repo, m.store),
		loadStatusCmd(context.Background(), m.repo, m.cfg, m.store.Tickets, id),
	)
}

// beginDelete opens the confirmation for the selected ticket.
func (m Model) beginDelete() (tea.Model, tea.Cmd) {
	t, ok := m.selectedTicket()
	if !ok {
		m.notice = "no ticket to delete"
		return m, nil
	}
	m.pendingDelete = t.ID
	m.screen = screenConfirmDelete
	m.notice = ""
	return m, nil
}

// dispatchConfirmDelete handles the y/n confirmation.
func (m Model) dispatchConfirmDelete(action Action) (tea.Model, tea.Cmd) {
	switch action {
	case ActionCancel:
		m.screen = screenDashboard
		m.pendingDelete = ""
		return m, nil

	case ActionConfirm:
		return m.doDelete()
	}
	return m, nil
}

// doDelete removes the pending ticket and persists. The lookup is by ID, not by
// cursor, so a stale cursor can never delete the wrong ticket.
func (m Model) doDelete() (tea.Model, tea.Cmd) {
	id := m.pendingDelete
	m.screen = screenDashboard
	m.pendingDelete = ""

	idx := -1
	for i, t := range m.store.Tickets {
		if t.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return m, nil // already gone; nothing to persist
	}

	m.store.Tickets = append(m.store.Tickets[:idx], m.store.Tickets[idx+1:]...)
	delete(m.expanded, id)
	m = m.clampCursor() // the ticket's rows are gone; keep the cursor in range
	m.notice = "deleted " + id
	return m, saveStateCmd(m.repo, m.store)
}

// cancelToDashboard drops any in-flight add/confirm and returns home.
func (m Model) cancelToDashboard() Model {
	m.screen = screenDashboard
	m.add = addFlow{}
	m.pendingDelete = ""
	m.notice = ""
	return m
}
