package ui

import (
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"drift/internal/store"
)

// Update is the single event sink. Key presses are resolved to a named action
// through the keymap before anything happens, so no branch here ever tests a raw
// key — that indirection is what makes area 12 a pure override.
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

	case tea.KeyMsg:
		if action, ok := m.keys.action(msg.String()); ok {
			return m.dispatch(action)
		}
		return m, nil
	}
	return m, nil
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

// dispatch runs one named action. Actions whose screens are not built yet report
// where they are headed rather than doing nothing silently.
func (m Model) dispatch(action Action) (tea.Model, tea.Cmd) {
	switch action {
	case ActionQuit:
		return m, tea.Quit

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
		m.notice = "add ticket arrives next session — hand-edit state.json to seed tickets for now"
		return m, nil

	case ActionDelete:
		m.notice = "delete arrives next session"
		return m, nil

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
