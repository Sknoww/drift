package ui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"drift/internal/git"
	"drift/internal/store"
)

// branchStatus is one branch's computed signal against its paired target.
type branchStatus struct {
	ahead, behind int
	known         bool  // false when the branch's target key is absent from config
	err           error // AheadBehind failed (e.g. the branch no longer exists locally)
}

// statusMsg carries a completed status sweep back into Update. One sweep
// computes the whole map so the view flips from "refreshing" to "current" in a
// single frame rather than filling in row by row.
type statusMsg struct {
	current  string // checked-out branch, "" when detached
	dirty    bool   // working-tree dirty — a property of the checked-out branch alone
	byKey    map[string]branchStatus
	err      error // a top-level probe (current branch / dirty) failed
	fetchErr error // set only on the fetch path, when the fetch itself failed
}

// statusKey identifies a branch's status within one ticket. A branch name can
// recur across tickets, so the ticket ID is part of the key (DESIGN.md §1).
func statusKey(ticketID, branch string) string {
	return ticketID + "\x00" + branch
}

// candidatesMsg carries a completed candidate-branch scan back into Update. The
// id is echoed so a scan that lands after the flow moved on can be discarded.
type candidatesMsg struct {
	id       string
	branches []string
	err      error
}

// loadCandidatesCmd lists the local branches whose name contains the ticket ID,
// off the UI thread. The user still confirms every match — this only pre-filters.
func loadCandidatesCmd(repo *git.Repo, id string) tea.Cmd {
	return func() tea.Msg {
		got, err := repo.CandidateBranches(context.Background(), id)
		return candidatesMsg{id: id, branches: got, err: err}
	}
}

// saveStateMsg reports the result of persisting state.json.
type saveStateMsg struct{ err error }

// saveStateCmd writes the store to disk asynchronously. The in-memory model is
// already updated by the time this runs; a failure is surfaced as a notice so
// the user can retry rather than lose the edit silently.
func saveStateCmd(repo *git.Repo, st store.Store) tea.Cmd {
	return func() tea.Msg {
		return saveStateMsg{err: store.SaveState(context.Background(), repo, st)}
	}
}

// loadStatusCmd sweeps every tracked branch against its target without fetching.
// It backs the refresh action and the initial load.
func loadStatusCmd(repo *git.Repo, cfg store.Config, tickets []store.Ticket) tea.Cmd {
	return func() tea.Msg {
		return sweep(context.Background(), repo, cfg, tickets, false)
	}
}

// fetchThenLoadCmd fetches remote-tracking refs first, then sweeps, so
// ahead/behind reflects the server. A failed fetch does not abort the sweep —
// stale-but-shown beats blank, and fetchErr tells the user the numbers are old.
func fetchThenLoadCmd(repo *git.Repo, cfg store.Config, tickets []store.Ticket) tea.Cmd {
	return func() tea.Msg {
		return sweep(context.Background(), repo, cfg, tickets, true)
	}
}

func sweep(ctx context.Context, repo *git.Repo, cfg store.Config, tickets []store.Ticket, doFetch bool) statusMsg {
	var msg statusMsg

	if doFetch {
		if err := repo.Fetch(ctx); err != nil {
			msg.fetchErr = err
		}
	}

	current, err := repo.CurrentBranch(ctx)
	if err != nil {
		msg.err = err
		return msg
	}
	msg.current = current

	dirty, err := repo.IsDirty(ctx)
	if err != nil {
		msg.err = err
		return msg
	}
	msg.dirty = dirty

	msg.byKey = make(map[string]branchStatus)
	for _, t := range tickets {
		for _, b := range t.Branches {
			key := statusKey(t.ID, b.Branch)
			target, ok := cfg.Target(b.TargetKey)
			if !ok {
				msg.byKey[key] = branchStatus{known: false}
				continue
			}
			ab, err := repo.AheadBehind(ctx, b.Branch, target.Ref)
			if err != nil {
				msg.byKey[key] = branchStatus{known: true, err: err}
				continue
			}
			msg.byKey[key] = branchStatus{ahead: ab.Ahead, behind: ab.Behind, known: true}
		}
	}
	return msg
}
