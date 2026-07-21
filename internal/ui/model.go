// Package ui is Drift's Bubble Tea dashboard: the ticket-oriented view over the
// git wrapper and the store. It follows the Elm shape — Model/Update/View with
// git work run as async Cmds — so the UI never blocks on a git call.
//
// This package holds the dashboard (roadmap area 3, read side). The add/pair and
// delete flows land next; the named-action layer in keys.go is already in place
// so those screens slot in without a keybinding retrofit.
package ui

import (
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"drift/internal/git"
	"drift/internal/store"
)

// screen is the surface the UI is currently on. The dashboard is the home;
// the add flow and delete confirmation are transient screens the model returns
// from to the dashboard. Every screen dispatches on named actions (keys.go).
type screen int

const (
	screenDashboard     screen = iota
	screenAddID                // add flow: ticket ID entry
	screenPairing              // add flow: candidate checklist (+ picker overlay)
	screenConfirmDelete        // y/n confirm before dropping a ticket
)

// Model is the whole dashboard state. The status map is keyed by
// statusKey(ticketID, branch) and recomputed by the refresh/fetch Cmds; the
// model never computes git signals inline.
type Model struct {
	repo  *git.Repo
	cfg   store.Config
	store store.Store

	keys   keymaps
	styles styles

	screen screen

	cursor   int             // index into cfg-ordered tickets; the selected ticket
	expanded map[string]bool // ticket ID -> whether its branches are shown

	input         textinput.Model // ticket ID entry, live only on screenAddID
	add           addFlow         // pairing state, live only on screenPairing
	pendingDelete string          // ticket ID awaiting delete confirmation

	status  map[string]branchStatus
	current string // checked-out branch, "" when detached
	dirty   bool   // working tree dirty — applies to the checked-out branch only

	loading bool
	spin    spinner.Model

	width, height int

	notice string // transient one-line hint or error under the panel
	err    error  // last status-sweep error, shown until the next good sweep

	targetKeyWidth int // widest configured Target.Key, for column alignment
}

// New builds the dashboard model over an already-loaded config and store. Git
// signals are not computed here — Init kicks off the first async sweep.
func New(repo *git.Repo, cfg store.Config, st store.Store) Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot

	return Model{
		repo:           repo,
		cfg:            cfg,
		store:          st,
		keys:           defaultKeymaps(),
		styles:         newStyles(),
		expanded:       make(map[string]bool),
		status:         make(map[string]branchStatus),
		loading:        true,
		spin:           sp,
		targetKeyWidth: widestTargetKey(cfg),
	}
}

// widestTargetKey drives the status-cluster column width. The target label is
// variable-width and unbounded in count, so the column is computed, never fixed
// (DESIGN.md §1).
func widestTargetKey(cfg store.Config) int {
	w := 0
	for _, t := range cfg.Targets {
		if len(t.Key) > w {
			w = len(t.Key)
		}
	}
	return w
}

// Init starts the spinner and the first status sweep.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spin.Tick,
		loadStatusCmd(m.repo, m.cfg, m.store.Tickets),
	)
}
