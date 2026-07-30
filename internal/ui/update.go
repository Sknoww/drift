package ui

import (
	"context"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Sknoww/drift/internal/store"
)

// Update is the single event sink. Key presses are resolved to a named action
// through the active screen's keymap before anything happens, so no branch here
// ever tests a raw key — that indirection is what makes area 12 a pure override.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.screen == screenDiff {
			m.diff.vp.Width = panelViewportWidth(m.styles, m.width)
			m.diff.vp.Height = diffViewportHeight(m.height)
		}
		// The help overlay needs nothing here: its pane is derived from the size
		// on every render, so a resize refits it by itself (help.go).
		return m, nil

	case spinner.TickMsg:
		// A running shelve keeps the spinner alive on its own account: the step it
		// is on is drawn with one, and the sequence is not a status sweep.
		if !m.loading && !m.shelve.active {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case statusMsg:
		return m.applyStatus(msg), nil

	case candidatesMsg:
		return m.applyCandidates(msg), nil

	case diffMsg:
		return m.applyDiff(msg), nil

	case declareMsg:
		return m.applyDeclare(msg)

	case declaredMsg:
		return m.applyDeclared(msg), nil

	case localOnlyMsg:
		return m.applyLocalOnly(msg)

	case localCandidatesMsg:
		return m.applyLocalCandidates(msg), nil

	case localHoldMsg:
		return m.applyLocalHold(msg)

	case shelveMsg:
		return m.applyShelve(msg)

	case remoteRefsMsg:
		return m.applyRemoteRefs(msg), nil

	case repointMsg:
		return m.applyRepoint(msg)

	case saveStateMsg:
		if msg.err != nil {
			m.notice = "save failed: " + msg.err.Error()
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Wherever a text field is live, everything else (cursor blink) drives it.
	if m.typing() {
		return m.feedField(msg)
	}
	return m, nil
}

// typing reports whether a text field is live, so an unbound key is a keystroke
// rather than a no-op. Four screens qualify: ID entry, where the whole screen is
// the field; the local-only note editor open over its list; the pairing
// checklist's filter field; and the re-point picker's filter field. The last two
// have to swallow `space`, `t`, `e` and 1–9 or an incremental query could never
// contain them — and a ref is exactly the kind of string that does.
func (m Model) typing() bool {
	return m.screen == screenAddID ||
		(m.screen == screenLocalOnly && m.local.note.open) ||
		(m.screen == screenPairing && !m.add.picker && m.add.filter.open) ||
		(m.screen == screenTargets && m.repoint.open && !m.repoint.confirm && m.repoint.filter.open)
}

// feedField routes a keystroke to whichever field typing() found live. The
// filter owns its own input rather than borrowing m.input: m.input still holds
// the ticket ID that got the user to this screen, and a filter that typed over
// it would be a bug waiting for the first person to press esc.
func (m Model) feedField(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.screen == screenPairing && m.add.filter.open {
		f, cmd := m.add.filter.typed(msg)
		m.add = m.add.applyFilter(f)
		return m, cmd
	}
	if m.screen == screenTargets && m.repoint.filter.open {
		f, cmd := m.repoint.filter.typed(msg)
		m.repoint = m.repoint.applyFilter(f)
		return m, cmd
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// handleKey resolves a key through the active screen's keymap. On the ID-entry
// screen a key that binds to no action is a keystroke for the text input.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// The help overlay is a read-only interruption: any key dismisses it and is
	// consumed, so a key pressed to close it never also acts on the screen
	// underneath. ctrl+c still quits, since it always does.
	//
	// The one carve-out is scrolling, and only while there *is* something to
	// scroll — the overlay is taller than a standard terminal on the busier
	// screens (help.go). On a screen whose help fits, "any key closes" holds
	// unqualified and j/k close it like anything else, so the footer never claims
	// a key does something it doesn't.
	if m.showHelp {
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if helpScrollKeys[msg.String()] && m.helpScrolls() {
			return m.scrollHelp(msg.String()), nil
		}
		m.showHelp = false
		return m, nil
	}
	if m.typing() {
		if action, ok := m.activeKeys().action(msg.String()); ok {
			return m.dispatch(action)
		}
		return m.feedField(msg)
	}
	// On the diff panel, a bound key steps between files or backs out; every
	// other key falls through to the viewport so its own j/k/arrows/pgup/pgdn
	// scroll the diff — the reason DefaultDiffKeys binds so few keys. The declare
	// overlay shadows that: while a choice is open j/k must move the cursor, so
	// nothing falls through to the viewport underneath.
	if m.screen == screenDiff {
		if m.diff.declare.open {
			if action, ok := m.activeKeys().action(msg.String()); ok {
				return m.dispatch(action)
			}
			return m, nil
		}
		if action, ok := m.activeKeys().action(msg.String()); ok {
			return m.dispatch(action)
		}
		var cmd tea.Cmd
		m.diff.vp, cmd = m.diff.vp.Update(msg)
		return m, cmd
	}
	if action, ok := m.activeKeys().action(msg.String()); ok {
		return m.dispatch(action)
	}
	return m, nil
}

// activeKeys is the keymap for the current screen — the picker overlay shadows
// the pairing keymap while it is open.
func (m Model) activeKeys() Keymap {
	switch m.screen {
	case screenAddID:
		return m.keys.addID
	case screenPairing:
		switch {
		case m.add.picker:
			return m.keys.picker
		case m.add.filter.open:
			// Only the filter's control keys act while it has focus; every other
			// key falls through typing() into the query.
			return m.keys.filter
		}
		return m.keys.pairing
	case screenConfirmDelete:
		return m.keys.confirmDelete
	case screenDiff:
		if m.diff.declare.open {
			return m.keys.declare
		}
		return m.keys.diff
	case screenLocalOnly:
		switch {
		case m.local.note.open:
			return m.keys.localNote
		case m.local.add.open:
			return m.keys.localAdd
		}
		return m.keys.localOnly
	case screenShelve:
		if m.shelve.confirm {
			// The stash-plan overlay shadows the report's keymap while it is open,
			// exactly as the declare overlay shadows the diff panel's.
			return m.keys.confirmStash
		}
		return m.keys.shelve
	case screenTargets:
		switch {
		case m.repoint.confirm:
			// The confirmation shadows the picker's keymap while it is open, exactly
			// as the stash plan shadows the shelve report's.
			return m.keys.confirmRepoint
		case m.repoint.open && m.repoint.filter.open:
			// Only the filter's control keys act while it has focus; every other key
			// falls through typing() into the query.
			return m.keys.filter
		case m.repoint.open:
			return m.keys.repoint
		}
		return m.keys.targets
	default:
		return m.keys.dashboard
	}
}

// applyStatus folds a completed sweep into the model. A result carrying a stale
// id — its sweep was superseded by a newer one, or cancelled mid-fetch — is
// dropped so a slow or hung fetch can never clobber current state.
func (m Model) applyStatus(msg statusMsg) Model {
	if msg.id != m.sweepID {
		return m
	}
	if m.fetchCancel != nil {
		m.fetchCancel() // this sweep landed; release the fetch context
		m.fetchCancel = nil
	}
	m.loading = false
	m.err = msg.err
	if msg.byKey != nil {
		m.status = msg.byKey
	}
	m.current = msg.current
	m.dirty = msg.dirty

	switch {
	case msg.err != nil:
		m.notice = "" // the error line renders msg.err on its own
	case msg.fetchErr != nil:
		m.notice = "fetch failed — showing last-known status: " + msg.fetchErr.Error()
	default:
		m.notice = ""
	}
	return m
}

// dispatch runs one named action, routed to the current screen. Quit is the one
// action that means the same thing everywhere, so it is handled up front.
func (m Model) dispatch(action Action) (tea.Model, tea.Cmd) {
	if action == ActionQuit {
		return m, tea.Quit
	}
	// Help is global for the same reason quit is: it means one thing everywhere,
	// and it reads the screen it was opened from rather than replacing it.
	if action == ActionHelp {
		m.showHelp = true
		m.helpOffset = 0 // each opening starts at the top, whatever the last one left
		return m, nil
	}
	switch m.screen {
	case screenAddID:
		return m.dispatchAddID(action)
	case screenPairing:
		return m.dispatchPairing(action)
	case screenConfirmDelete:
		return m.dispatchConfirmDelete(action)
	case screenDiff:
		return m.dispatchDiff(action)
	case screenLocalOnly:
		return m.dispatchLocalOnly(action)
	case screenShelve:
		return m.dispatchShelve(action)
	case screenTargets:
		return m.dispatchTargets(action)
	default:
		return m.dispatchDashboard(action)
	}
}

// dispatchDashboard runs one named action on the home screen. Actions whose
// screens are not built yet report where they are headed rather than doing
// nothing silently.
func (m Model) dispatchDashboard(action Action) (tea.Model, tea.Cmd) {
	switch action {
	case ActionMoveUp:
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case ActionMoveDown:
		if m.cursor < len(m.visibleRows())-1 {
			m.cursor++
		}
		return m, nil

	case ActionToggleExpand:
		// enter opens whatever is selected: a ticket toggles its branches, a
		// branch opens its unmergeable diff. Collapsing shortens the visible list,
		// so re-clamp the cursor afterward.
		row, ok := m.selectedRow()
		if !ok {
			return m, nil
		}
		if row.isBranch() {
			return m.openDiff(row)
		}
		t := m.store.Tickets[row.ticket]
		m.expanded[t.ID] = !m.expanded[t.ID]
		return m.clampCursor(), nil

	case ActionRefresh:
		return m.startSweep(false)

	case ActionFetch:
		return m.startSweep(true)

	case ActionAdd:
		return m.beginAdd()

	case ActionDelete:
		return m.beginDelete()

	case ActionCancel:
		return m.cancelFetch(), nil

	case ActionLocalOnly:
		return m.openLocalOnly()

	case ActionShelve:
		return m.beginShelve()

	case ActionUpdate:
		return m.beginUpdate()

	case ActionTargets:
		return m.openTargets()
	}
	return m, nil
}

// startSweep flips into the loading state and fires the right sweep Cmd,
// re-arming the spinner alongside it. Only the fetch path is cancellable — a
// plain refresh is local and fast, so it runs on a background context.
func (m Model) startSweep(fetch bool) (tea.Model, tea.Cmd) {
	m, id := m.supersedeSweeps()
	m.loading = true
	m.notice = ""
	if fetch {
		ctx, cancel := context.WithCancel(context.Background())
		m.fetchCancel = cancel
		m.notice = "fetching… (esc to cancel)"
		return m, tea.Batch(m.spin.Tick, fetchThenLoadCmd(ctx, m.repo, m.cfg, m.store.Tickets, id))
	}
	return m, tea.Batch(m.spin.Tick, loadStatusCmd(context.Background(), m.repo, m.cfg, m.store.Tickets, id))
}

// supersedeSweeps invalidates any in-flight sweep and returns the id the next
// sweep Cmd must stamp. It cancels a running fetch (killing its git process) and
// bumps sweepID so the old sweep's result is discarded on arrival.
func (m Model) supersedeSweeps() (Model, int) {
	if m.fetchCancel != nil {
		m.fetchCancel()
		m.fetchCancel = nil
	}
	m.sweepID++
	return m, m.sweepID
}

// cancelFetch aborts an in-flight fetch and hands control back at once: the git
// process is killed and its now-stale sweep discarded, so a hung fetch never
// traps the user. A no-op when no fetch is running (esc on an idle dashboard).
func (m Model) cancelFetch() Model {
	if m.fetchCancel == nil {
		return m
	}
	m, _ = m.supersedeSweeps() // cancels + bumps sweepID so the result is dropped
	m.loading = false
	m.notice = "fetch canceled"
	return m
}

// selectedTicket returns the ticket the cursor points at — the ticket itself
// when a ticket row is selected, or the parent ticket when a branch row is. So
// delete, from a branch row, drops the branch's ticket; the confirmation names
// exactly what goes, so nothing is a surprise.
func (m Model) selectedTicket() (store.Ticket, bool) {
	row, ok := m.selectedRow()
	if !ok {
		return store.Ticket{}, false
	}
	return m.store.Tickets[row.ticket], true
}
