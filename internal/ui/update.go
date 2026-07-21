package ui

import (
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

// applyStatus folds a completed sweep into the model.
func (m Model) applyStatus(msg statusMsg) Model {
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

	case ActionLocalOnly:
		m.notice = "local-only changes arrive in area 6"
		return m, nil
	}
	return m, nil
}

// startSweep flips into the loading state and fires the right sweep Cmd,
// re-arming the spinner alongside it.
func (m Model) startSweep(fetch bool) (tea.Model, tea.Cmd) {
	m.loading = true
	m.notice = ""
	sweep := loadStatusCmd(m.repo, m.cfg, m.store.Tickets)
	if fetch {
		m.notice = "fetching…"
		sweep = fetchThenLoadCmd(m.repo, m.cfg, m.store.Tickets)
	}
	return m, tea.Batch(m.spin.Tick, sweep)
}

// selectedTicket returns the ticket the cursor points at.
func (m Model) selectedTicket() (store.Ticket, bool) {
	if m.cursor < 0 || m.cursor >= len(m.store.Tickets) {
		return store.Ticket{}, false
	}
	return m.store.Tickets[m.cursor], true
}
