package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Sknoww/drift/internal/git"
	"github.com/Sknoww/drift/internal/store"
)

// The one-key shelve sequence (roadmap area 7, docs/specs/shelve-sequence.md):
// pull the target, merge it into the checked-out branch, and put your work back,
// as one keypress. It is the payoff for storing the ticket → (target, branch)
// grouping in the first place — Drift already knows every argument the four
// commands need.
//
// It runs as a *chain* of Cmds rather than one long-running Cmd, for two reasons.
// The user can see which step is happening, which matters because the back half
// of the sequence mutates the working tree. And every decision about where to
// stop lives in Update, over plain data, where it can be tested without a repo.

// shelveStep is one step of the sequence, in run order.
type shelveStep int

const (
	stepReady   shelveStep = iota // preconditions that need to ask git
	stepPull                      // fetch this target's ref, and only this one
	stepHolds                     // recompute behind, then the local-only collision check
	stepStash                     // the first step that touches the working tree
	stepMerge                     // merge the target in
	stepRestore                   // pop the stash back
)

// shelveSteps is the display order and wording. Everything up to stepStash is
// read-only — see the "read-only until the last possible moment" rule in the
// spec — so a sequence that refuses has nothing to undo.
var shelveSteps = []struct {
	step shelveStep
	what string
}{
	{stepReady, "check the repo is ready"},
	{stepPull, "pull the target"},
	{stepHolds, "check local-only holds"},
	{stepStash, "stash your work"},
	{stepMerge, "merge the target in"},
	{stepRestore, "put your work back"},
}

// shelveOutcome is where the sequence ended up. Every value but shelveRunning is
// terminal, and every terminal value except shelveLanded is a *handoff*: Drift
// stops, names what it found, and leaves the reconciliation to the human.
type shelveOutcome int

const (
	shelveRunning  shelveOutcome = iota
	shelveLanded                 // merged and restored, clean
	shelveCurrent                // the target hadn't moved; nothing was touched
	shelveHeld                   // the target changed a path you hold locally
	shelveReverted               // merge conflict: aborted and restored, no trace left
	shelveHandoff                // pop conflict: the merge landed, the stash is retained
	shelveStopped                // refused by a precondition, or a git call failed
)

// shelveFile is one path in a halt's report. The note is the local-only
// annotation for a held collision; unmergeable flags the paths git can never
// merge, which is what decides whether the fix is a text merge or a trip to an
// external tool.
type shelveFile struct {
	path        string
	note        string
	unmergeable bool
}

// shelveState is the live sequence. It is also the report left behind after it
// ends: the screen stays up, showing what happened, until the user backs out.
type shelveState struct {
	active bool
	seq    int // monotonic; a step landing under a stale id is discarded

	ticketID  string
	branch    string
	targetKey string
	targetRef string

	step    shelveStep // the step running now, or the one that ended the sequence
	outcome shelveOutcome

	stashOID string
	files    []shelveFile
	reason   string // what happened, in the user's terms
	next     string // the git command that resolves it
	err      error

	// ctx is the read-only head of the chain's context and cancel releases it;
	// cancellable gates whether esc may. Only stepReady and stepPull are
	// cancellable — once the stash is taken there is no cancelling into an
	// undefined middle (spec), so the flag goes false while cancel stays, to be
	// called when the sequence ends. Held on the state rather than passed along
	// because the thing being cancelled is a UI-lived operation, exactly like the
	// dashboard's fetchCancel.
	ctx         context.Context
	cancel      context.CancelFunc
	cancellable bool
}

// shelveMsg carries one completed step back into Update. One message type for
// the whole chain rather than one per step: Update advances the sequence by
// looking at which step landed, and a single type keeps that in one place.
type shelveMsg struct {
	seq  int
	step shelveStep
	err  error

	skipped  bool         // stepPull: the target ref belongs to no remote
	behind   int          // stepHolds: recomputed against the freshly pulled ref
	files    []shelveFile // stepHolds: held collisions · stepMerge/stepRestore: conflicts
	stashOID string       // stepStash: "" when the tree was clean and nothing was stashed
	restored error        // stepMerge: the abort-and-restore's own failure, if any
}

// beginShelve starts the sequence on the selected branch row.
//
// Every precondition that can be answered from the model is answered here, so a
// refusal is instant and the report screen never opens on a sequence that was
// never going to run. The ones that need to ask git are stepReady.
func (m Model) beginShelve() (tea.Model, tea.Cmd) {
	if m.shelve.active {
		m.notice = "a shelve is already running — one at a time"
		return m, nil
	}
	row, ok := m.selectedRow()
	if !ok || !row.isBranch() {
		m.notice = "select a branch row to shelve — s works on a branch, not a ticket"
		return m, nil
	}
	t := m.store.Tickets[row.ticket]
	br := t.Branches[row.branch]

	if m.current == "" {
		m.notice = "detached HEAD — check out " + br.Branch + " first"
		return m, nil
	}
	// Drift never checks anything out, and that is correctness rather than
	// squeamishness: a stash belongs to the branch it was taken on, so a
	// cross-branch sequence would have to carry uncommitted work over a branch
	// boundary to put it back (spec, "Scope").
	if br.Branch != m.current {
		m.notice = br.Branch + " isn't checked out — git switch " + br.Branch + " first"
		return m, nil
	}
	target, known := m.cfg.Target(br.TargetKey)
	if !known {
		m.notice = "unknown target " + quote(br.TargetKey) + " — fix the pairing or config.json"
		return m, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.shelve = shelveState{
		active:      true,
		seq:         m.shelve.seq + 1,
		ticketID:    t.ID,
		branch:      br.Branch,
		targetKey:   target.Key,
		targetRef:   target.Ref,
		step:        stepReady,
		ctx:         ctx,
		cancel:      cancel,
		cancellable: true,
	}
	m.screen = screenShelve
	m.notice = ""
	return m, tea.Batch(m.spin.Tick, shelveReadyCmd(ctx, m.repo, m.shelve.seq))
}

// applyShelve folds one completed step in and fires the next, or ends the
// sequence. A message carrying a stale seq belongs to a sequence the user
// already cancelled, so it is dropped rather than acted on.
func (m Model) applyShelve(msg shelveMsg) (tea.Model, tea.Cmd) {
	if !m.shelve.active || msg.seq != m.shelve.seq {
		return m, nil
	}
	if msg.err != nil {
		return m.endShelve(shelveStopped, msg.err.Error(), ""), nil
	}

	ctx := context.Background() // only the read-only head of the chain is cancellable
	switch msg.step {
	case stepReady:
		m.shelve.step = stepPull
		return m, shelvePullCmd(m.shelve.ctx, m.repo, m.shelve.seq, m.shelve.targetRef)

	case stepPull:
		if msg.skipped {
			// A target that is not remote-tracking has nothing to pull. Saying so
			// beats silently pretending the merge is against fresh data.
			m.notice = m.shelve.targetRef + " isn't a remote-tracking ref — merging it as it stands"
		}
		m.shelve.cancellable = false // everything past here mutates or commits to mutating
		m.shelve.step = stepHolds
		return m, shelveHoldsCmd(ctx, m.repo, m.store, m.shelve.seq, m.shelve.branch, m.shelve.targetRef)

	case stepHolds:
		// Recomputed against the ref we just pulled: a sequence that stashed
		// first, then discovered there was nothing to merge, would have churned
		// the working tree to accomplish nothing.
		if msg.behind == 0 {
			return m.endShelve(shelveCurrent, "", ""), nil
		}
		if len(msg.files) > 0 {
			m.shelve.files = msg.files
			return m.endShelve(shelveHeld,
				"the target changed files you hold on this machine",
				"release the hold (l) or reconcile by hand, then run s again"), nil
		}
		m.shelve.step = stepStash
		return m, shelveStashCmd(ctx, m.repo, m.shelve.seq, m.shelve.stashMessage())

	case stepStash:
		m.shelve.stashOID = msg.stashOID
		m.shelve.step = stepMerge
		return m, shelveMergeCmd(ctx, m.repo, m.cfg, m.shelve.seq, m.shelve.targetRef, msg.stashOID)

	case stepMerge:
		if len(msg.files) > 0 {
			m.shelve.files = msg.files
			if msg.restored != nil {
				return m.endShelve(shelveStopped,
					"the merge conflicted, and putting your work back failed: "+msg.restored.Error(),
					"git stash list — your work is still stashed"), nil
			}
			// Aborted and restored: the sequence either lands whole or leaves no
			// trace, so the user is byte-for-byte where they started.
			return m.endShelve(shelveReverted,
				"both sides committed to these files, so the merge was rolled back",
				"reconcile by hand, then run s again"), nil
		}
		if m.shelve.stashOID == "" {
			return m.endShelve(shelveLanded, "", ""), nil // clean tree: nothing to put back
		}
		m.shelve.step = stepRestore
		return m, shelveRestoreCmd(ctx, m.repo, m.cfg, m.shelve.seq, m.shelve.stashOID)

	case stepRestore:
		if len(msg.files) > 0 {
			// Deliberately *not* restored, unlike a merge conflict. `git stash pop`
			// retains its entry when it conflicts, so nothing is at risk — and this
			// is the reconciliation point the sequence was run to reach. Undoing it
			// would undo the one thing that went right.
			m.shelve.files = msg.files
			return m.endShelve(shelveHandoff,
				"the target's version and your uncommitted work both changed these files",
				"reconcile by hand — your work is still in the stash until you drop it"), nil
		}
		return m.endShelve(shelveLanded, "", ""), nil
	}
	return m, nil
}

// endShelve closes the sequence out, leaving the report on screen. A landed
// sequence re-sweeps, so the dashboard behind it shows the new reality rather
// than the numbers that prompted the shelve.
func (m Model) endShelve(outcome shelveOutcome, reason, next string) Model {
	if m.shelve.cancel != nil {
		m.shelve.cancel()
		m.shelve.cancel = nil
	}
	m.shelve.active = false
	m.shelve.cancellable = false
	m.shelve.outcome = outcome
	m.shelve.reason = reason
	m.shelve.next = next
	return m
}

// stashMessage identifies Drift's stash in `git stash list`, so a user who lands
// on a handoff can find their work by eye rather than by counting entries.
func (s shelveState) stashMessage() string {
	return fmt.Sprintf("drift: shelve %s ← %s", s.branch, s.targetKey)
}

// dispatchShelve handles the report screen. Cancel is the only verb: while the
// mutating steps run it is refused rather than obeyed, since there is no
// cancelling into an undefined middle.
func (m Model) dispatchShelve(action Action) (tea.Model, tea.Cmd) {
	if action != ActionCancel {
		return m, nil
	}
	switch {
	case m.shelve.active && m.shelve.cancellable:
		m.shelve.seq++ // any in-flight step's result is now stale and will be dropped
		m = m.endShelve(shelveStopped, "canceled", "")
		m.screen = screenDashboard
		m.notice = "shelve canceled — nothing was touched"
		return m, nil
	case m.shelve.active:
		m.notice = "the sequence is mid-flight — it stops on its own"
		return m, nil
	}
	m.screen = screenDashboard
	m.notice = ""
	if m.shelve.outcome == shelveLanded {
		return m.startSweep(false)
	}
	return m, nil
}

// --- commands -------------------------------------------------------------

// shelveReadyCmd is the half of the preconditions that has to ask git: whether
// the repo is already in the middle of something. Drift will not stack a
// sequence on top of a merge or a rebase — the repo is not in the state the
// sequence assumes, and stacking on it turns one problem into two.
func shelveReadyCmd(ctx context.Context, repo *git.Repo, seq int) tea.Cmd {
	return func() tea.Msg {
		op, err := repo.OperationInProgress(ctx)
		if err != nil {
			return shelveMsg{seq: seq, step: stepReady, err: err}
		}
		if op != "" {
			return shelveMsg{seq: seq, step: stepReady,
				err: errors.New(op + " is already in progress — finish or abort it first")}
		}
		return shelveMsg{seq: seq, step: stepReady}
	}
}

// shelvePullCmd updates the one target ref being merged. Drift never checks a
// target out, so "pull the target" is: fetch its remote-tracking ref, then merge
// that — the two halves of `git pull`, against a ref it never has to visit.
func shelvePullCmd(ctx context.Context, repo *git.Repo, seq int, targetRef string) tea.Cmd {
	return func() tea.Msg {
		remote, branch, ok, err := repo.RemoteRef(ctx, targetRef)
		if err != nil {
			return shelveMsg{seq: seq, step: stepPull, err: err}
		}
		if !ok {
			return shelveMsg{seq: seq, step: stepPull, skipped: true}
		}
		return shelveMsg{seq: seq, step: stepPull, err: repo.FetchRef(ctx, remote, branch)}
	}
}

// shelveHoldsCmd is the last read-only step: recompute how far behind the branch
// is against the ref just pulled, then check the one hazard no mechanism can
// avoid — the target changed a file you hold on this machine.
//
// Drift does not rely on git's behavior for that case (it varies by version —
// abort vs. clobber), it checks first, and it checks *before* the stash so a halt
// leaves nothing to undo (docs/specs/local-only-changes.md).
func shelveHoldsCmd(ctx context.Context, repo *git.Repo, st store.Store, seq int, branch, targetRef string) tea.Cmd {
	return func() tea.Msg {
		ab, err := repo.AheadBehind(ctx, branch, targetRef)
		if err != nil {
			return shelveMsg{seq: seq, step: stepHolds, err: err}
		}
		if ab.Behind == 0 {
			return shelveMsg{seq: seq, step: stepHolds}
		}

		incoming, err := repo.ChangedFiles(ctx, branch, targetRef)
		if err != nil {
			return shelveMsg{seq: seq, step: stepHolds, err: err}
		}
		// Git's own flags are the source of truth for what is held, exactly as on
		// the local-only screen: the skip-worktree bit for tracked paths, Drift's
		// fenced block in info/exclude for untracked ones. Read fresh, never
		// assumed from the store, which holds only the notes.
		skipped, err := repo.SkipWorktreeFiles(ctx)
		if err != nil {
			return shelveMsg{seq: seq, step: stepHolds, err: err}
		}
		excluded, err := repo.ExcludedPaths(ctx)
		if err != nil {
			return shelveMsg{seq: seq, step: stepHolds, err: err}
		}
		held := toSet(skipped)
		for _, p := range excluded {
			held[p] = true
		}

		var files []shelveFile // incoming order, so the report is deterministic
		for _, p := range incoming {
			if held[p] {
				files = append(files, shelveFile{path: p, note: st.LocalOnlyNote(p)})
			}
		}
		return shelveMsg{seq: seq, step: stepHolds, behind: ab.Behind, files: files}
	}
}

// shelveStashCmd takes the stash. Plain push, never -u or -a: untracked and
// skip-worktree files are what area 6 holds on this machine, and a plain stash
// cannot see either, which is how they ride the sequence through with no
// re-apply step. An empty stashOID means the tree was clean and there is nothing
// to put back later.
func shelveStashCmd(ctx context.Context, repo *git.Repo, seq int, message string) tea.Cmd {
	return func() tea.Msg {
		oid, err := repo.Stash(ctx, message)
		return shelveMsg{seq: seq, step: stepStash, stashOID: oid, err: err}
	}
}

// shelveMergeCmd merges the target and, on a conflict, undoes the whole thing:
// abort the merge, put the stash back, report what collided. The recovery lives
// inside this one Cmd on purpose — it is a single atomic "never mind", not two
// steps a user could be shown standing between.
//
// So the mutating half of the sequence either lands whole or leaves no trace.
// The one thing Drift will not do is stack a pop on a failed abort: a failed
// abort means the repo is not in the state Drift thought it was.
func shelveMergeCmd(ctx context.Context, repo *git.Repo, cfg store.Config, seq int, targetRef, stashOID string) tea.Cmd {
	return func() tea.Msg {
		conflicts, err := repo.Merge(ctx, targetRef)
		if err != nil {
			return shelveMsg{seq: seq, step: stepMerge, err: err}
		}
		if len(conflicts) == 0 {
			return shelveMsg{seq: seq, step: stepMerge}
		}

		msg := shelveMsg{seq: seq, step: stepMerge, files: classify(ctx, repo, cfg, conflicts)}
		if abortErr := repo.MergeAbort(ctx); abortErr != nil {
			msg.restored = abortErr
			return msg
		}
		if stashOID != "" {
			if _, popErr := repo.StashPop(ctx, stashOID); popErr != nil {
				msg.restored = popErr
			}
		}
		return msg
	}
}

// shelveRestoreCmd puts the stashed work back over the merged target. A conflict
// here is the destination, not a failure: git retains the stash entry when a pop
// conflicts, so the work is still safe and the user is standing exactly where the
// hand reconciliation happens.
func shelveRestoreCmd(ctx context.Context, repo *git.Repo, cfg store.Config, seq int, stashOID string) tea.Cmd {
	return func() tea.Msg {
		conflicts, err := repo.StashPop(ctx, stashOID)
		if err != nil {
			return shelveMsg{seq: seq, step: stepRestore, err: err}
		}
		return shelveMsg{seq: seq, step: stepRestore, files: classify(ctx, repo, cfg, conflicts)}
	}
}

// classify tags each conflicting path with whether git can never merge it, by
// the same hybrid rule detection uses: git's own `-merge` attribute unioned with
// the config globs (CONTEXT.md). It is what decides whether the reconciliation is
// an ordinary text merge or a trip to an external tool, which is the single most
// useful thing the report can say. A failed check-attr degrades the flag only —
// the conflict list itself is still worth showing.
func classify(ctx context.Context, repo *git.Repo, cfg store.Config, paths []string) []shelveFile {
	attr, _ := repo.CheckAttrMerge(ctx, paths)
	files := make([]shelveFile, len(paths))
	for i, p := range paths {
		files[i] = shelveFile{path: p, unmergeable: attr[p] || cfg.MatchesUnmergeable(p)}
	}
	return files
}

// --- view -----------------------------------------------------------------

// shelveView is the report: the sequence's steps with where it got to, then what
// it found. It stays on screen after the sequence ends, because a handoff the
// user has to act on is not a notice line.
func (m Model) shelveView() string {
	s := m.shelve
	lines := []string{
		m.styles.hint.Render(fmt.Sprintf("Shelve %s onto %s", s.targetKey, s.branch)),
		m.styles.help.Render("  " + s.targetRef + " → " + s.branch + "   (" + s.ticketID + ")"),
		"",
	}
	for _, step := range shelveSteps {
		lines = append(lines, m.shelveStepRow(step.step, step.what))
	}
	if body := m.shelveOutcomeBody(); body != "" {
		lines = append(lines, "", body)
	}
	return m.screenView(strings.Join(lines, "\n"), m.shelveHelp())
}

// shelveStepRow draws one step with its state. The steps before the stash are
// read-only, so a sequence that stops in that half has visibly touched nothing —
// which is the reassurance the ordering was designed to give.
func (m Model) shelveStepRow(step shelveStep, what string) string {
	s := m.shelve
	switch {
	case step < s.step:
		return "  " + m.styles.sync.Render("✓") + "  " + m.styles.help.Render(what)
	case step > s.step:
		return "  " + m.styles.help.Render("·  "+what)
	case s.active:
		return "  " + m.spin.View() + " " + what
	case s.outcome == shelveLanded || s.outcome == shelveCurrent:
		return "  " + m.styles.sync.Render("✓") + "  " + m.styles.help.Render(what)
	case s.outcome == shelveStopped:
		return "  " + m.styles.errText.Render("✗") + "  " + what
	default:
		return "  " + m.styles.unmerge.Render("■") + "  " + what
	}
}

// shelveOutcomeBody is what the sequence found — the headline, the files, and the
// git command that resolves it. Every halt names its next action: Drift's job is
// to hand back a well-lit problem, not a stack trace.
func (m Model) shelveOutcomeBody() string {
	s := m.shelve
	if s.active {
		return ""
	}

	var head string
	switch s.outcome {
	case shelveLanded:
		head = m.styles.sync.Render("✓ " + s.targetKey + " merged in, your work is back")
	case shelveCurrent:
		head = m.styles.sync.Render("✓ already current — " + s.targetKey + " hasn't moved. Nothing was touched")
	case shelveHeld:
		head = m.styles.unmerge.Render("■ stopped before touching anything")
	case shelveReverted:
		head = m.styles.unmerge.Render("■ rolled back — you are exactly where you started")
	case shelveHandoff:
		head = m.styles.unmerge.Render("■ merged, but your work needs reconciling by hand")
	case shelveStopped:
		head = m.styles.errText.Render("✗ stopped")
	}

	lines := []string{head}
	if s.reason != "" {
		lines = append(lines, m.styles.help.Render("  "+s.reason))
	}
	for _, f := range s.files {
		lines = append(lines, "  "+m.shelveFileRow(f))
	}
	if s.next != "" {
		lines = append(lines, "", m.styles.hint.Render("  next: "+s.next))
	}
	return strings.Join(lines, "\n")
}

// shelveFileRow draws one reported path. The unmergeable flag leads, because it
// is what tells the user whether this is a text merge or a trip to an external
// tool — and it is the whole reason the sequence stops rather than pressing on.
func (m Model) shelveFileRow(f shelveFile) string {
	row := m.styles.branch.Render(f.path)
	if f.unmergeable {
		row += "  " + m.styles.unmerge.Render("⚠ unmergeable")
	}
	if f.note != "" {
		row += "  " + m.styles.help.Render("— "+f.note)
	}
	return row
}

func (m Model) shelveHelp() string {
	if m.shelve.active && m.shelve.cancellable {
		return helpLine(m.styles, m.width, nil, []string{"esc cancel", "? help", "q quit"})
	}
	if m.shelve.active {
		return helpLine(m.styles, m.width,
			[]string{"running — it stops on its own"}, []string{"? help", "q quit"})
	}
	return helpLine(m.styles, m.width, nil, []string{"esc back", "? help", "q quit"})
}
