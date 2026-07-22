// Package ui is Drift's Bubble Tea dashboard: the ticket-oriented view over the
// git wrapper and the store. It follows the Elm shape — Model/Update/View with
// git work run as async Cmds — so the UI never blocks on a git call.
//
// This package holds the dashboard (roadmap area 3, read side). The add/pair and
// delete flows land next; the named-action layer in keys.go is already in place
// so those screens slot in without a keybinding retrofit.
package ui

import (
	"context"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
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
	screenDiff                 // area 5: the unmergeable diff panel for one branch
)

// rowRef names one selectable row on the dashboard. The cursor addresses a flat
// list of these — ticket headlines plus, under an expanded ticket, its branch
// rows — so a branch can be selected in its own right (area 5's diff is
// per-branch: MVP2 and MVP3 can hold different versions of the same file, so a
// ticket-scoped diff would conflate them). A ticket row carries branch == -1.
type rowRef struct {
	ticket int // index into store.Tickets
	branch int // index into that ticket's Branches, or -1 for the ticket headline
}

func (r rowRef) isBranch() bool { return r.branch >= 0 }

// diffState is the live area-5 diff panel: one branch's unmergeable collisions
// and the file currently shown. Diffs load lazily — the sweep records only the
// colliding paths, and each file's text is fetched on demand and cached here, so
// a branch with many collisions costs nothing until its diff is opened.
type diffState struct {
	ticketID  string
	branch    string
	targetKey string
	targetRef string               // origin/<target>, the tip the diff is taken against
	files     []string             // colliding unmergeable paths, in detection order
	cursor    int                  // index into files: the file on screen
	cache     map[string]diffEntry // path -> loaded diff, absent while still loading
	vp        viewport.Model       // scrolls a diff taller or wider than the panel
}

// diffEntry is one file's fetched diff, or the error fetching it.
type diffEntry struct {
	content string
	err     error
}

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

	cursor   int             // index into visibleRows(); the selected ticket or branch
	expanded map[string]bool // ticket ID -> whether its branches are shown

	input         textinput.Model // ticket ID entry, live only on screenAddID
	add           addFlow         // pairing state, live only on screenPairing
	pendingDelete string          // ticket ID awaiting delete confirmation
	diff          diffState       // unmergeable diff panel, live only on screenDiff

	status  map[string]branchStatus
	current string // checked-out branch, "" when detached
	dirty   bool   // working tree dirty — applies to the checked-out branch only

	loading bool
	spin    spinner.Model

	// sweepID monotonically tags each status sweep; a sweep whose result carries
	// a stale id (superseded by a newer sweep, or cancelled) is discarded rather
	// than folded in. fetchCancel kills the in-flight fetch's git process — set
	// only while a fetch is running, nil otherwise, so it doubles as "a fetch is
	// in flight" and gates esc-to-cancel.
	sweepID     int
	fetchCancel context.CancelFunc

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
		loadStatusCmd(context.Background(), m.repo, m.cfg, m.store.Tickets, m.sweepID),
	)
}

// visibleRows is the flat list the cursor moves over, in render order: every
// ticket headline, and the branch rows of each expanded ticket right beneath it.
// Non-selectable lines (the "no branches" hint, the delete prompt) are drawn but
// never listed here, so the cursor can never land on them.
func (m Model) visibleRows() []rowRef {
	var rows []rowRef
	for ti, t := range m.store.Tickets {
		rows = append(rows, rowRef{ticket: ti, branch: -1})
		if m.expanded[t.ID] {
			for bi := range t.Branches {
				rows = append(rows, rowRef{ticket: ti, branch: bi})
			}
		}
	}
	return rows
}

// selectedRow is the row the cursor points at. Reports false when there is
// nothing to select (no tickets, or a cursor left stale by a collapse).
func (m Model) selectedRow() (rowRef, bool) {
	rows := m.visibleRows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return rowRef{}, false
	}
	return rows[m.cursor], true
}

// ticketRowIndex is the visible-row index of a ticket's headline, used to move
// the cursor to a specific ticket (e.g. the one just added) without assuming its
// position — branch rows shift every ticket after an expanded one.
func (m Model) ticketRowIndex(ti int) int {
	for i, r := range m.visibleRows() {
		if r.ticket == ti && !r.isBranch() {
			return i
		}
	}
	return 0
}

// clampCursor keeps the cursor inside the visible-row list after the list
// shrinks — collapsing a ticket removes its branch rows from under it.
func (m Model) clampCursor() Model {
	if n := len(m.visibleRows()); m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	return m
}
