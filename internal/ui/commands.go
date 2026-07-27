package ui

import (
	"context"
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"drift/internal/git"
	"drift/internal/store"
)

// branchStatus is one branch's computed signal against its paired target.
type branchStatus struct {
	ahead, behind int
	known         bool  // false when the branch's target key is absent from config
	err           error // AheadBehind failed (e.g. the branch no longer exists locally)

	// unmergeable is the paths that changed on both this branch and its target
	// since they diverged AND that Git must never merge (area 5). Empty unless the
	// target moved past the merge base (behind>0), since only then is there an
	// incoming change to collide with. In target-changed order, so the diff panel
	// lists them deterministically.
	unmergeable []collision
}

// collision is one unmergeable path, plus how Drift knows it is unmergeable.
//
// declared is Git's own answer — the `-merge` attribute, which every Git
// command respects — as against a config glob, which only Drift can see. That
// difference is the entire point of the declare flow: it is the state declaring
// changes, so the diff panel shows it per file and a write visibly flips it.
type collision struct {
	path     string
	declared bool
}

// paths flattens collisions for a call that only needs the names.
func paths(cs []collision) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.path
	}
	return out
}

// statusMsg carries a completed status sweep back into Update. One sweep
// computes the whole map so the view flips from "refreshing" to "current" in a
// single frame rather than filling in row by row.
type statusMsg struct {
	id       int    // the sweepID this result belongs to; a stale id is discarded
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

// diffMsg carries one loaded file diff back into Update. branch and targetRef
// identify the diff session it belongs to, so a diff that lands after the user
// backed out of the panel (or switched branches) is discarded rather than shown
// against the wrong branch.
type diffMsg struct {
	branch    string
	targetRef string
	path      string
	content   string
	err       error
}

// loadDiffCmd fetches one unmergeable file's incoming upstream diff off the UI
// thread: `git diff branch...targetRef -- path`, the exact change to reconcile.
func loadDiffCmd(repo *git.Repo, branch, targetRef, path string) tea.Cmd {
	return func() tea.Msg {
		out, err := repo.FileDiff(context.Background(), branch, targetRef, path)
		return diffMsg{branch: branch, targetRef: targetRef, path: path, content: out, err: err}
	}
}

// declareMsg reports a completed `-merge` declaration. The destination rides
// along so the notice can name where the line landed without re-deriving it.
type declareMsg struct {
	dest git.AttrDest
	decl git.AttrDeclaration
	err  error
}

// declareCmd writes the chosen pattern's `-merge` declaration off the UI thread
// (area 5, part 2). It is file I/O, not a git shell-out, but it runs as a Cmd
// like everything else: the model stays the only thing Update mutates.
func declareCmd(repo *git.Repo, dest git.AttrDest, pattern string) tea.Cmd {
	return func() tea.Msg {
		decl, err := repo.DeclareUnmergeable(context.Background(), dest, pattern)
		return declareMsg{dest: dest, decl: decl, err: err}
	}
}

// declaredMsg carries a re-read of Git's `-merge` attribute for the files on
// the diff panel. branch and targetRef identify the session it belongs to, the
// same guard the diff itself uses against landing on the wrong branch.
type declaredMsg struct {
	branch    string
	targetRef string
	byPath    map[string]bool
	err       error
}

// recheckDeclaredCmd asks Git again which of these paths it has been told never
// to merge. It runs after a declaration lands, so the panel's per-file state
// comes from Git rather than from what Drift assumes its own write achieved —
// the same rule detection follows, and the reason a glob that covers several of
// the listed files updates all of them at once.
func recheckDeclaredCmd(repo *git.Repo, branch, targetRef string, files []string) tea.Cmd {
	return func() tea.Msg {
		got, err := repo.CheckAttrMerge(context.Background(), files)
		return declaredMsg{branch: branch, targetRef: targetRef, byPath: got, err: err}
	}
}

// localOnlyMsg carries Git's answer to "what is held back from commits"
// (area 6). It is the whole held set every time, not a delta: Git's flags are
// the source of truth, so the list is rebuilt rather than patched.
type localOnlyMsg struct {
	held []heldPath
	err  error
}

// loadLocalOnlyCmd reads the held set off the UI thread, from both primitives:
// the skip-worktree bit for tracked paths and Drift's fenced block in
// info/exclude for untracked ones. The two are merged into one path-sorted list,
// because the user marked *changes* — the mechanism is a detail of each row, not
// a reason to split the screen in two.
func loadLocalOnlyCmd(repo *git.Repo) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		skipped, err := repo.SkipWorktreeFiles(ctx)
		if err != nil {
			return localOnlyMsg{err: err}
		}
		excluded, err := repo.ExcludedPaths(ctx)
		if err != nil {
			return localOnlyMsg{err: err}
		}

		held := make([]heldPath, 0, len(skipped)+len(excluded))
		for _, p := range skipped {
			held = append(held, heldPath{path: p, tracked: true})
		}
		for _, p := range excluded {
			held = append(held, heldPath{path: p})
		}
		sort.Slice(held, func(i, j int) bool { return held[i].path < held[j].path })
		return localOnlyMsg{held: held}
	}
}

// localCandidatesMsg carries the working-tree scan that backs the add flow.
type localCandidatesMsg struct {
	changes []git.WorkingChange
	err     error
}

// loadLocalCandidatesCmd lists what Git currently sees changed, so the add flow
// offers real changes to hold rather than a path typed from memory.
func loadLocalCandidatesCmd(repo *git.Repo) tea.Cmd {
	return func() tea.Msg {
		got, err := repo.WorkingChanges(context.Background())
		return localCandidatesMsg{changes: got, err: err}
	}
}

// localHoldMsg reports a completed hold or release. hold distinguishes the two
// so the notice can say which happened without the caller tracking it.
type localHoldMsg struct {
	path string
	hold bool
	err  error
}

// holdLocalCmd holds a path, routed by whether Git tracks it — the user marked
// a change, and this is where the mechanism is chosen for them.
func holdLocalCmd(repo *git.Repo, path string, tracked bool) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		var err error
		if tracked {
			err = repo.SetSkipWorktree(ctx, path)
		} else {
			err = repo.AddExclude(ctx, path)
		}
		return localHoldMsg{path: path, hold: true, err: err}
	}
}

// releaseLocalCmd undoes a hold through whichever primitive holds it. A tracked
// path's edits reappear as ordinary working-tree changes — they were never
// lost, only hidden.
func releaseLocalCmd(repo *git.Repo, path string, tracked bool) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		var err error
		if tracked {
			err = repo.ClearSkipWorktree(ctx, path)
		} else {
			err = repo.RemoveExclude(ctx, path)
		}
		return localHoldMsg{path: path, err: err}
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
// It backs the refresh action and the initial load. The ctx is background for a
// plain refresh (local and fast, nothing to cancel); id tags the result so a
// superseded sweep is discarded.
func loadStatusCmd(ctx context.Context, repo *git.Repo, cfg store.Config, tickets []store.Ticket, id int) tea.Cmd {
	return func() tea.Msg {
		msg := sweep(ctx, repo, cfg, tickets, false)
		msg.id = id
		return msg
	}
}

// fetchThenLoadCmd fetches remote-tracking refs first, then sweeps, so
// ahead/behind reflects the server. A failed fetch does not abort the sweep —
// stale-but-shown beats blank, and fetchErr tells the user the numbers are old.
// The ctx is cancellable so esc can abort a hung fetch; id tags the result so
// the cancelled sweep's message is dropped.
func fetchThenLoadCmd(ctx context.Context, repo *git.Repo, cfg store.Config, tickets []store.Ticket, id int) tea.Cmd {
	return func() tea.Msg {
		msg := sweep(ctx, repo, cfg, tickets, true)
		msg.id = id
		return msg
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

	// Working-tree edits count as local edits (per the area-5 decision), but only
	// the checked-out branch can have any — skip-worktree aside, one worktree has
	// one index. Fetch the set once and union it in for that branch alone. A
	// failure here just drops the working-tree half; committed edits still count.
	var workTree map[string]bool
	if current != "" {
		if mod, wErr := repo.WorkingTreeModified(ctx); wErr == nil {
			workTree = toSet(mod)
		}
	}

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
			st := branchStatus{ahead: ab.Ahead, behind: ab.Behind, known: true}
			// The target only has an incoming change to collide with when it moved
			// past the merge base — so gate detection on behind>0 and reuse the
			// count we already have. A detection error degrades the marker only,
			// never the ahead/behind row: the numbers stay useful on their own.
			if ab.Behind > 0 {
				var wt map[string]bool
				if b.Branch == current {
					wt = workTree
				}
				st.unmergeable, _ = detectUnmergeable(ctx, repo, cfg, b.Branch, target.Ref, wt)
			}
			msg.byKey[key] = st
		}
	}
	return msg
}

// detectUnmergeable resolves one branch's unmergeable collisions against its
// target: the paths changed on both sides since they diverged that Git must
// never merge. The branch side is its committed changes unioned with workTree
// (the working-tree edits, passed only for the checked-out branch). The
// unmergeable filter is the hybrid rule — `git check-attr merge` (the
// .gitattributes declaration) unioned with the config globs (CONTEXT.md).
func detectUnmergeable(ctx context.Context, repo *git.Repo, cfg store.Config, branch, targetRef string, workTree map[string]bool) ([]collision, error) {
	targetChanged, err := repo.ChangedFiles(ctx, branch, targetRef)
	if err != nil {
		return nil, err
	}
	if len(targetChanged) == 0 {
		return nil, nil
	}
	branchChanged, err := repo.ChangedFiles(ctx, targetRef, branch)
	if err != nil {
		return nil, err
	}
	branchSide := toSet(branchChanged)
	for p := range workTree {
		branchSide[p] = true
	}

	var collisions []string // target-changed order, so the panel list is stable
	for _, p := range targetChanged {
		if branchSide[p] {
			collisions = append(collisions, p)
		}
	}
	if len(collisions) == 0 {
		return nil, nil
	}

	attr, err := repo.CheckAttrMerge(ctx, collisions)
	if err != nil {
		return nil, err
	}
	var out []collision
	for _, p := range collisions {
		switch {
		case attr[p]:
			// Git's own declaration. Recorded as such: it is what the diff panel
			// reports per file, and what declaring exists to bring about.
			out = append(out, collision{path: p, declared: true})
		case cfg.MatchesUnmergeable(p):
			out = append(out, collision{path: p})
		}
	}
	return out, nil
}

func toSet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, s := range items {
		set[s] = true
	}
	return set
}
