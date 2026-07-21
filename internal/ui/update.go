package ui

import (
	"context"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"drift/internal/store"
)

// Update is the single event sink. Key presses are resolved to a named action
// through the active screen's keymap before anything happens, so no branch here
// ever tests a raw key — that indirection is what makes area 12 a pure override.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case spinner.TickMsg:
		if !m.loading {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case statusMsg:
		return m.applyStatus(msg), nil

	case candidatesMsg:
		return m.applyCandidates(msg), nil

	case saveStateMsg:
		if msg.err != nil {
			m.notice = "save failed: " + msg.err.Error()
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// On the ID-entry screen, everything else (cursor blink) drives the input.
	if m.screen == screenAddID {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// handleKey resolves a key through the active screen's keymap. On the ID-entry
// screen a key that binds to no action is a keystroke for the text input.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.screen == screenAddID {
		if action, ok := m.keys.addID.action(msg.String()); ok {
			return m.dispatch(action)
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
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
		if m.add.picker {
			return m.keys.picker
		}
		return m.keys.pairing
	case screenConfirmDelete:
		return m.keys.confirmDelete
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
	switch m.screen {
	case screenAddID:
		return m.dispatchAddID(action)
	case screenPairing:
		return m.dispatchPairing(action)
	case screenConfirmDelete:
		return m.dispatchConfirmDelete(action)
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
		if m.cursor < len(m.store.Tickets)-1 {
			m.cursor++
		}
		return m, nil

	case ActionToggleExpand:
		if t, ok := m.selectedTicket(); ok {
			m.expanded[t.ID] = !m.expanded[t.ID]
		}
		return m, nil

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
		m.notice = "local-only changes arrive in area 6"
		return m, nil
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

// selectedTicket returns the ticket the cursor points at.
func (m Model) selectedTicket() (store.Ticket, bool) {
	if m.cursor < 0 || m.cursor >= len(m.store.Tickets) {
		return store.Ticket{}, false
	}
	return m.store.Tickets[m.cursor], true
}
