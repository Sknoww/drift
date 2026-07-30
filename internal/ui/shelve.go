package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Sknoww/drift/internal/git"
	"github.com/Sknoww/drift/internal/store"
)

// The one-key sequences (roadmap areas 7 and 17, docs/specs/shelve-sequence.md).
// They are the payoff for storing the ticket → (target, branch) grouping in the
// first place — Drift already knows every argument the commands need.
//
// Two verbs share this state machine, differing by commitment. `s` shelves:
// pull the target, merge it into the checked-out branch, put your work back, and
// publish nothing. `u` updates: the same merge, but it checks the branch out,
// pulls the branch's own upstream first, pushes the result, and returns you to
// where you were standing. One machine rather than two, because the halts, the
// stash identity rules and the report are the same in both — only the step list
// and the commitment differ.
//
// It runs as a *chain* of Cmds rather than one long-running Cmd, for two reasons.
// The user can see which step is happening, which matters because the back half
// of the sequence mutates the working tree. And every decision about where to
// stop lives in Update, over plain data, where it can be tested without a repo.

// shelveMode is which verb is running. The distinction is commitment, not
// mechanism: modeShelve leaves the branch ahead of its own remote by design,
// which is exactly what makes the two legible on the dashboard rather than only
// in the help.
type shelveMode int

const (
	modeShelve shelveMode = iota // s: merge the target in, here, and publish nothing
	modeUpdate                   // u: carry the branch all the way, including the push
)

// shelveStep is one step of the sequence, in run order. Four of them belong to
// modeUpdate alone; the constants are ordered so a single "how far have we got"
// comparison works for either verb.
type shelveStep int

const (
	stepReady    shelveStep = iota // preconditions that need to ask git
	stepPull                       // fetch this target's ref, and only this one
	stepHolds                      // recompute behind, then the local-only collision check
	stepStash                      // the first step that touches the working tree
	stepSwitch                     // update: check the branch out
	stepUpstream                   // update: merge the branch's own upstream
	stepMerge                      // merge the target in
	stepPush                       // update: publish the branch
	stepReturn                     // update: go back to where the user was standing
	stepRestore                    // pop the stash back
)

// shelveStepLabel is one row of the progress list: the step and how it is worded
// on screen.
type shelveStepLabel struct {
	step shelveStep
	what string
}

// steps is the display order and wording for the verb that is running.
// Everything up to stepStash is read-only — see the "read-only until the last
// possible moment" rule in the spec — so a sequence that refuses has nothing to
// undo. That the rule survives the extra update steps is the whole reason both
// fetches are hoisted above the stash: `u` merges two refs, and both of them can
// be brought up to date without touching the working tree.
func (s shelveState) steps() []shelveStepLabel {
	out := []shelveStepLabel{{stepReady, "check the repo is ready"}}
	if s.mode == modeUpdate {
		out = append(out, shelveStepLabel{stepPull, "fetch the target and " + s.branch})
	} else {
		out = append(out, shelveStepLabel{stepPull, "pull the target"})
	}
	out = append(out,
		shelveStepLabel{stepHolds, "check local-only holds"},
		shelveStepLabel{stepStash, "stash your work"},
	)
	if s.mode == modeUpdate {
		if s.leaves() {
			out = append(out, shelveStepLabel{stepSwitch, "check out " + s.branch})
		}
		out = append(out, shelveStepLabel{stepUpstream, "pull " + s.branch + "'s own upstream"})
	}
	out = append(out, shelveStepLabel{stepMerge, "merge the target in"})
	if s.mode == modeUpdate {
		out = append(out, shelveStepLabel{stepPush, "publish " + s.branch})
		if s.leaves() {
			out = append(out, shelveStepLabel{stepReturn, "return to " + s.from})
		}
	}
	return append(out, shelveStepLabel{stepRestore, "put your work back"})
}

// leaves reports whether the sequence has to check out away from where the user
// was standing — and therefore owes them a return. Known before anything runs,
// so the step list never changes shape mid-sequence.
func (s shelveState) leaves() bool { return s.mode == modeUpdate && s.branch != s.from }

// shelveOutcome is where the sequence ended up. Every value but shelveRunning is
// terminal, and every terminal value except shelveLanded is a *handoff*: Drift
// stops, names what it found, and leaves the reconciliation to the human.
type shelveOutcome int

const (
	shelveRunning  shelveOutcome = iota
	shelveLanded                 // merged and restored, clean
	shelveCurrent                // nothing had moved; nothing was touched
	shelveHeld                   // the target changed a path you hold locally
	shelveReverted               // merge conflict: aborted and restored, no trace left
	shelveHandoff                // the merge landed, but something still needs a human
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
	mode   shelveMode

	ticketID  string
	branch    string
	targetKey string
	targetRef string

	// The update sequence's bookkeeping. from is where the user was standing when
	// they pressed the key, and getting them back there is part of the sequence
	// rather than a courtesy — every halt path unwinds it too. switched records
	// whether Drift has actually left yet, so an unwind knows whether it owes a
	// return or only a pop.
	from        string
	upstreamRef string // the branch's own upstream, "" when it tracks nothing
	pushRemote  string // remote owning that upstream
	pushBranch  string // the branch's name on that remote, which need not match
	switched    bool

	// confirm is the plan overlay, open while the user decides. It is a *prompt*,
	// not a warning: the machinery under it is the same machinery every other path
	// uses, and what is being agreed to is that the sequence runs at all.
	//
	// 17b opened it for one case — leaving a branch with uncommitted work on it —
	// because being stashed without having agreed to it is a surprise. 19a widened
	// it to every `u`, on the step that actually needed gating: the push is the
	// only step in the sequence with no unwind and the only one other people can
	// see, and a stash is recoverable and local by comparison. `u` always intends
	// to reach the remote, so it always asks; whether there will turn out to be
	// anything to send is not knowable until the fetches land, and predicting it
	// here from the dashboard's last sweep is the kind of claim this package
	// refuses to make elsewhere.
	confirm bool

	// dirty is what stepReady found in the working tree, kept because the plan has
	// to state the stash and the return only when there is work to stash. It
	// settles the *wording*, never the gate — that is the mode alone.
	dirty bool

	// planUpstream is where the push will land and planNoUpstream is the third
	// answer (a branch that has never been published), both as the dashboard's
	// last sweep saw them. They exist for the overlay and nothing else: naming a
	// destination is the half of the plan a bare "publish it" would leave the user
	// to assume, and an upstream under a different name is exactly the assumption
	// worth breaking. Unknown — an empty ref with no ⊘ — states no destination
	// rather than a guessed one.
	planUpstream   string
	planNoUpstream bool

	step    shelveStep // the step running now, or the one that ended the sequence
	outcome shelveOutcome
	skipped map[shelveStep]bool // steps that had nothing to do, drawn as such
	problem map[shelveStep]bool // steps that did not do their job, drawn as such

	stashOID    string
	pushed      git.PushOutcome
	files       []shelveFile
	reason      string // what happened, in the user's terms
	publish     string // what became of the push, when it is not simply "done"
	publishNext string // the command that publishes it by hand
	next        string // the git command that resolves the halt
	err         error

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
	next string // the fix to name when err ends the sequence

	skipped  bool         // this step had nothing to do, and the report says so
	dirty    bool         // stepReady: whether there is anything to stash
	behind   int          // stepHolds: recomputed against the freshly pulled ref
	upBehind int          // stepHolds: how far behind its own upstream the branch is
	upAhead  int          // stepHolds: and how far ahead — the half `u` publishes
	files    []shelveFile // stepHolds: held collisions · merges: conflicts
	stashOID string       // stepStash: "" when the tree was clean and nothing was stashed
	restored error        // the unwind's own failure, if putting the user back went wrong
	pushed   git.PushOutcome
	rejected bool // stepPush: the remote branch moved on; a handoff, never a force

	upstreamRef, pushRemote, pushBranch string // stepPull: the branch's own upstream
}

// beginShelve starts `s` on the selected branch row: merge the target in, here,
// and publish nothing.
func (m Model) beginShelve() (tea.Model, tea.Cmd) {
	return m.beginSequence(modeShelve)
}

// beginUpdate starts `u` on the selected branch row: the same merge, carried all
// the way — the branch's own upstream pulled first, the result pushed, and the
// user returned to the branch they were standing on.
func (m Model) beginUpdate() (tea.Model, tea.Cmd) {
	return m.beginSequence(modeUpdate)
}

// beginSequence starts whichever verb was pressed.
//
// Every precondition that can be answered from the model is answered here, so a
// refusal is instant and the report screen never opens on a sequence that was
// never going to run. The ones that need to ask git are stepReady.
func (m Model) beginSequence(mode shelveMode) (tea.Model, tea.Cmd) {
	verb := "shelve"
	if mode == modeUpdate {
		verb = "update"
	}
	if m.shelve.active {
		m.notice = "a sequence is already running — one at a time"
		return m, nil
	}
	row, ok := m.selectedRow()
	if !ok || !row.isBranch() {
		m.notice = "select a branch row — " + verb + " works on a branch, not a ticket"
		return m, nil
	}
	t := m.store.Tickets[row.ticket]
	br := t.Branches[row.branch]

	// A detached HEAD has nothing to merge into, and for `u` it is also nowhere to
	// come back to: the return is part of the sequence, so a starting point that
	// is not a branch is refused rather than approximated.
	if m.current == "" {
		m.notice = "detached HEAD — check out " + br.Branch + " first"
		return m, nil
	}
	// `s` stays exactly as it shipped: the checked-out branch only. That is not a
	// leftover — it is what keeps the local-only path reachable for the case where
	// you want to see the merge before it goes anywhere (roadmap area 17).
	if mode == modeShelve && br.Branch != m.current {
		m.notice = br.Branch + " isn't checked out — press u to update it, or git switch " + br.Branch
		return m, nil
	}
	target, known := m.cfg.Target(br.TargetKey)
	if !known {
		m.notice = "unknown target " + quote(br.TargetKey) + " — fix the pairing or config.json"
		return m, nil
	}

	// What the last sweep knows about where this branch publishes to. Read here,
	// with the rest of what the plan is stated from, so the overlay never reaches
	// back into the dashboard's state mid-sequence.
	st := m.status[br.Branch]

	ctx, cancel := context.WithCancel(context.Background())
	m.shelve = shelveState{
		active:         true,
		seq:            m.shelve.seq + 1,
		mode:           mode,
		ticketID:       t.ID,
		branch:         br.Branch,
		targetKey:      target.Key,
		targetRef:      target.Ref,
		from:           m.current,
		planUpstream:   st.upstreamRef,
		planNoUpstream: st.noUpstream,
		step:           stepReady,
		skipped:        make(map[shelveStep]bool),
		problem:        make(map[shelveStep]bool),
		ctx:            ctx,
		cancel:         cancel,
		cancellable:    true,
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
	// A step past the stash rolls the sequence back before it reports, so what
	// arrives here is already the state after the rollback — unless the rollback
	// is what failed. That case outranks whatever prompted it: the work is still
	// stashed and the user may be standing somewhere they did not choose, and
	// nothing else in the report matters as much as saying so.
	if msg.restored != nil {
		reason := "putting you back failed: " + msg.restored.Error()
		switch {
		case msg.err != nil:
			reason = msg.err.Error() + " — and " + reason
		case len(msg.files) > 0:
			m.shelve.files = msg.files
			reason = "the merge conflicted, and " + reason
		}
		return m.endShelve(shelveStopped, reason, m.shelve.recoverHint()), nil
	}
	if msg.err != nil {
		return m.endShelve(shelveStopped, msg.err.Error(), msg.next), nil
	}

	ctx := context.Background() // only the read-only head of the chain is cancellable
	switch msg.step {
	case stepReady:
		// `u` states its plan and waits, every time. 17b asked about the stash and
		// answered that correctly; 19a widened it to the step that actually needed
		// gating — the push, the one step with no unwind and the only one other
		// people can see. A wrong target is then visible at the one moment it can
		// still be stopped for free, which is the whole of what the v0.3.0 incident
		// needed and did not get.
		//
		// Asked here, at the last moment before anything at all happens, rather
		// than once the fetches have narrowed down what there is to do. Two reasons,
		// and they point the same way. What the plan states — which refs, which
		// remote, whose work is on the tree — is already fully known, and nothing a
		// fetch can return would change what the user is agreeing to. And a verb
		// whose whole promise is one keypress must not stop for input *in the
		// middle*: press u, press y, walk away. The cost is a prompt on a sequence
		// that then finds nothing to do, which ends by saying nothing was touched.
		//
		// `s` gets no prompt and needs none: it publishes nothing, so every step it
		// takes is local and undone by the same unwind every halt already runs.
		m.shelve.dirty = msg.dirty
		if m.shelve.mode == modeUpdate {
			m.shelve.confirm = true
			return m, nil
		}
		// msg.dirty settles the wording above and nothing else: whether there is
		// anything to *put back* is stepStash's own answer, and marking the steps
		// from here would be predicting a result git has not given yet.
		return m.startPull()

	case stepPull:
		if msg.skipped {
			// A target that is not remote-tracking has nothing to pull. Saying so
			// beats silently pretending the merge is against fresh data.
			m.notice = m.shelve.targetRef + " isn't a remote-tracking ref — merging it as it stands"
		}
		m.shelve.upstreamRef = msg.upstreamRef
		m.shelve.pushRemote, m.shelve.pushBranch = msg.pushRemote, msg.pushBranch
		m.shelve.cancellable = false // everything past here mutates or commits to mutating
		m.shelve.step = stepHolds
		return m, shelveHoldsCmd(ctx, m.repo, m.store, m.shelve.seq, m.shelve.mode,
			m.shelve.branch, m.shelve.targetRef, m.shelve.upstreamRef)

	case stepHolds:
		// Recomputed against the refs we just fetched: a sequence that stashed
		// first, then discovered there was nothing to merge, would have churned
		// the working tree to accomplish nothing.
		if done, model := m.nothingToDo(msg); done {
			return model, nil
		}
		if len(msg.files) > 0 {
			m.shelve.files = msg.files
			return m.endShelve(shelveHeld,
				"the incoming changes touch files you hold on this machine",
				"release the hold (l) or reconcile by hand, then run it again"), nil
		}
		m.shelve.step = stepStash
		return m, shelveStashCmd(ctx, m.repo, m.shelve.seq, m.shelve.stashMessage())

	case stepStash:
		m.shelve.stashOID = msg.stashOID
		if msg.stashOID == "" {
			m.shelve.skipped[stepStash] = true
			m.shelve.skipped[stepRestore] = true
		}
		if m.shelve.mode == modeShelve {
			m.shelve.step = stepMerge
			return m, shelveMergeCmd(ctx, m.repo, m.cfg, m.shelve.seq, m.shelve.targetRef, m.shelve.unwind())
		}
		if m.shelve.leaves() {
			m.shelve.step = stepSwitch
			return m, shelveSwitchCmd(ctx, m.repo, m.shelve.seq, m.shelve.branch, m.shelve.unwind())
		}
		m.shelve.skipped[stepSwitch] = true
		m.shelve.step = stepUpstream
		return m, shelveUpstreamCmd(ctx, m.repo, m.cfg, m.shelve.seq, m.shelve.upstreamRef, m.shelve.unwind())

	case stepSwitch:
		// Drift is now standing somewhere the user did not put it, and owes them
		// the way back on every path from here.
		m.shelve.switched = true
		m.shelve.step = stepUpstream
		return m, shelveUpstreamCmd(ctx, m.repo, m.cfg, m.shelve.seq, m.shelve.upstreamRef, m.shelve.unwind())

	case stepUpstream:
		if msg.skipped {
			m.shelve.skipped[stepUpstream] = true
		}
		if len(msg.files) > 0 {
			// The branch diverged from itself: its upstream holds commits that
			// conflict with the ones here. That is a genuine halt — it is not the
			// target's doing, and no amount of merging the target will settle it.
			m.shelve.files = msg.files
			return m.endShelve(shelveReverted,
				m.shelve.branch+" and its upstream have both moved, and they conflict",
				"reconcile "+m.shelve.branch+" against "+m.shelve.upstreamRef+" by hand, then run u again"), nil
		}
		m.shelve.step = stepMerge
		return m, shelveMergeCmd(ctx, m.repo, m.cfg, m.shelve.seq, m.shelve.targetRef, m.shelve.unwind())

	case stepMerge:
		if len(msg.files) > 0 {
			// Aborted, returned and restored: the sequence either lands whole or
			// leaves no trace, so the user is byte-for-byte where they started.
			m.shelve.files = msg.files
			return m.endShelve(shelveReverted,
				"both sides committed to these files, so the merge was rolled back",
				"reconcile by hand, then run it again"), nil
		}
		if m.shelve.mode == modeShelve {
			return m.afterMutations(ctx)
		}
		m.shelve.step = stepPush
		return m, shelvePushCmd(ctx, m.repo, m.shelve.seq,
			m.shelve.pushRemote, m.shelve.branch, m.shelve.pushBranch, m.shelve.unwind())

	case stepPush:
		m = m.applyPush(msg)
		if m.shelve.leaves() {
			m.shelve.step = stepReturn
			return m, shelveReturnCmd(ctx, m.repo, m.shelve.seq, m.shelve.from)
		}
		m.shelve.skipped[stepReturn] = true
		return m.afterMutations(ctx)

	case stepReturn:
		m.shelve.switched = false // back where the user left us; nothing further is owed
		return m.afterMutations(ctx)

	case stepRestore:
		if len(msg.files) > 0 {
			// Deliberately *not* restored, unlike a merge conflict. `git stash pop`
			// retains its entry when it conflicts, so nothing is at risk — and this
			// is the reconciliation point the sequence was run to reach. Undoing it
			// would undo the one thing that went right.
			m.shelve.files = msg.files
			return m.endShelve(shelveHandoff,
				"the merged content and your uncommitted work both changed these files",
				"reconcile by hand — your work is still in the stash until you drop it"), nil
		}
		return m.finish(), nil
	}
	return m, nil
}

// startPull moves the sequence off the preconditions and onto the fetches. Its
// own method because two paths reach it — a sequence that needed no permission,
// and one the user has just granted it — and they must be the same path: the
// confirmation gates *whether* the sequence runs, never how.
func (m Model) startPull() (tea.Model, tea.Cmd) {
	m.shelve.step = stepPull
	return m, shelvePullCmd(m.shelve.ctx, m.repo, m.shelve.seq, m.shelve.mode, m.shelve.branch, m.shelve.targetRef)
}

// nothingToDo ends the sequence when the freshly-fetched refs say there is
// nothing for it to do, before anything is stashed. What counts as nothing
// differs by verb, and that difference *is* the difference between them: `s` is
// done when the target has not moved, while `u` also owes the branch's own
// upstream a pull and the remote a push.
func (m Model) nothingToDo(msg shelveMsg) (bool, Model) {
	s := m.shelve
	if s.mode == modeShelve {
		if msg.behind == 0 {
			return true, m.endShelve(shelveCurrent, "", "")
		}
		return false, m
	}
	if msg.behind > 0 || msg.upBehind > 0 || msg.upAhead > 0 {
		return false, m
	}
	// Nothing to merge and nothing ahead. If the branch has no upstream at all,
	// that is not "up to date" — it is unpublished, and saying so is the whole
	// point of a verb whose job is to publish.
	if s.upstreamRef == "" {
		return true, m.endShelve(shelveStopped,
			s.branch+" has no upstream, so there is nothing to pull into it and nowhere to publish it",
			"git push -u <remote> "+s.branch)
	}
	return true, m.endShelve(shelveCurrent, "", "")
}

// applyPush records what became of the publish. Neither problem it can report is
// fatal to the sequence: the branch is updated and merged locally either way, and
// only the publish did not happen — so the sequence still returns the user home
// and still pops, and the report is what says the branch stayed on this machine.
func (m Model) applyPush(msg shelveMsg) Model {
	switch {
	case msg.skipped:
		m.shelve.problem[stepPush] = true
		m.shelve.publish = m.shelve.branch + " has no upstream, so nothing was published"
		m.shelve.publishNext = "git push -u <remote> " + m.shelve.branch
	case msg.rejected:
		m.shelve.problem[stepPush] = true
		// Someone else's commit is in the way — exactly the class of thing Drift
		// stops and hands back rather than resolving. Never a force.
		m.shelve.publish = "the push was rejected: " + m.shelve.pushRemote + "/" +
			m.shelve.pushBranch + " moved on while this ran"
		m.shelve.publishNext = "git pull --rebase=false " + m.shelve.pushRemote + " " +
			m.shelve.pushBranch + ", then push"
	default:
		m.shelve.pushed = msg.pushed
	}
	return m
}

// afterMutations runs the tail every successful path shares: put the work back
// if there is any, otherwise finish here.
func (m Model) afterMutations(ctx context.Context) (tea.Model, tea.Cmd) {
	if m.shelve.stashOID == "" {
		return m.finish(), nil // clean tree: nothing to put back
	}
	m.shelve.step = stepRestore
	return m, shelveRestoreCmd(ctx, m.repo, m.cfg, m.shelve.seq, m.shelve.stashOID)
}

// finish closes a sequence that reached the end. It landed unless the publish
// half did not happen — a branch merged locally and left on this machine is not
// the thing `u` promises, so it reports as a handoff rather than a tick.
func (m Model) finish() Model {
	if m.shelve.publish != "" {
		return m.endShelve(shelveHandoff, "", m.shelve.publishNext)
	}
	return m.endShelve(shelveLanded, "", "")
}

// endShelve closes the sequence out, leaving the report on screen.
//
// A halt is flagged on the step it happened at, so the list points at the thing
// that went wrong wherever it sits rather than assuming the last step reached is
// it. A sequence that ran all the way to the end with only the publish
// outstanding is exactly that case: applyPush already flagged the push, and
// every step after it did its job and is entitled to say so.
func (m Model) endShelve(outcome shelveOutcome, reason, next string) Model {
	if reason != "" && outcome != shelveLanded && outcome != shelveCurrent {
		m.shelve.problem[m.shelve.step] = true
	}
	if m.shelve.cancel != nil {
		m.shelve.cancel()
		m.shelve.cancel = nil
	}
	m.shelve.active = false
	m.shelve.cancellable = false
	m.shelve.confirm = false // whatever ended it, there is nothing left to agree to
	m.shelve.outcome = outcome
	m.shelve.reason = reason
	m.shelve.next = next
	return m
}

// unwind is what this sequence owes the user if it stops here: the branch to
// return to, and the stash to put back. Derived from the live state rather than
// carried along, so it can never describe a rollback the sequence has outgrown.
func (s shelveState) unwind() unwindPlan {
	p := unwindPlan{stashOID: s.stashOID}
	if s.switched {
		p.back = s.from
	}
	return p
}

// recoverHint names the commands that finish an unwind Drift could not, so the
// one path where the user is left somewhere they did not ask to be still hands
// them the way back rather than a stack trace.
func (s shelveState) recoverHint() string {
	if s.switched {
		return "git switch " + s.from + ", then git stash pop — your work is still stashed"
	}
	return "git stash list — your work is still stashed"
}

// stashMessage identifies Drift's stash in `git stash list`, so a user who lands
// on a handoff can find their work by eye rather than by counting entries.
func (s shelveState) stashMessage() string {
	verb := "shelve"
	if s.mode == modeUpdate {
		verb = "update"
	}
	return fmt.Sprintf("drift: %s %s ← %s", verb, s.branch, s.targetKey)
}

// dispatchShelve handles the report screen. Cancel is the only verb once the
// sequence is running: while the mutating steps run it is refused rather than
// obeyed, since there is no cancelling into an undefined middle.
//
// The plan overlay adds the one place Confirm means anything here. It needs no
// cancel case of its own: the sequence is still on its read-only head, so
// declining is exactly the cancel the screen already had, down to the notice
// saying nothing was touched.
func (m Model) dispatchShelve(action Action) (tea.Model, tea.Cmd) {
	if m.shelve.confirm && action == ActionConfirm {
		m.shelve.confirm = false
		return m.startPull()
	}
	if action != ActionCancel {
		return m, nil
	}
	switch {
	case m.shelve.active && m.shelve.cancellable:
		m.shelve.seq++ // any in-flight step's result is now stale and will be dropped
		m = m.endShelve(shelveStopped, "canceled", "")
		m.screen = screenDashboard
		m.notice = "canceled — nothing was touched"
		return m, nil
	case m.shelve.active:
		m.notice = "the sequence is mid-flight — it stops on its own"
		return m, nil
	}
	m.screen = screenDashboard
	m.notice = ""
	// Re-sweep whenever history moved, so the dashboard behind the report shows
	// the new reality rather than the numbers that prompted the sequence.
	if m.shelve.outcome == shelveLanded || m.shelve.outcome == shelveHandoff {
		return m.startSweep(false)
	}
	return m, nil
}

// --- commands -------------------------------------------------------------

// unwindPlan is what a halted sequence owes the user: the branch to go back to
// ("" when it never left) and the stash to put back ("" when the tree was
// clean). A merge in flight is rolled back first, and unwind finds that out by
// asking rather than being told.
type unwindPlan struct {
	back     string
	stashOID string
}

// unwind puts the user back where they started, in the one order that works:
// roll back the merge, return to the branch they were on, then pop. It is what
// makes "every halt path unwinds too" true rather than aspirational.
//
// The merge is aborted only if one is actually in flight, and any operation
// found there is necessarily Drift's own — stepReady refused to start on top of
// somebody else's. That probe is what lets every post-stash halt share one
// rollback, including the ones where the merge failed without conflicting and so
// left nothing to abort.
//
// It stops at the first failure and reports it, never stacking the next step on
// top: a rollback that is already not going to plan is exactly when carrying on
// turns one problem into two.
func unwind(ctx context.Context, repo *git.Repo, p unwindPlan) error {
	op, err := repo.OperationInProgress(ctx)
	if err != nil {
		return err
	}
	if op != "" {
		if err := repo.MergeAbort(ctx); err != nil {
			return err
		}
	}
	if p.back != "" {
		if err := repo.Checkout(ctx, p.back); err != nil {
			return err
		}
	}
	if p.stashOID != "" {
		if _, err := repo.StashPop(ctx, p.stashOID); err != nil {
			return err
		}
	}
	return nil
}

// shelveReadyCmd is the half of the preconditions that has to ask git: whether
// the repo is already in the middle of something, and whether there is anything
// to stash. Drift will not stack a sequence on top of a merge or a rebase — the
// repo is not in the state the sequence assumes, and stacking on it turns one
// problem into two.
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
		dirty, err := repo.IsDirty(ctx)
		return shelveMsg{seq: seq, step: stepReady, dirty: dirty, err: err}
	}
}

// shelvePullCmd updates the refs the sequence is about to merge. Drift never
// checks a target out, so "pull the target" is: fetch its remote-tracking ref,
// then merge that — the two halves of `git pull`, against a ref it never has to
// visit. `u` adds the branch's own upstream, on the same split.
//
// Both fetches happen here, above the stash, which is what keeps the whole
// read-only prefix intact: fetching is how the numbers the next step refuses on
// become true, and none of it touches the working tree.
func shelvePullCmd(ctx context.Context, repo *git.Repo, seq int, mode shelveMode, branch, targetRef string) tea.Cmd {
	return func() tea.Msg {
		msg := shelveMsg{seq: seq, step: stepPull}
		remote, rb, ok, err := repo.RemoteRef(ctx, targetRef)
		if err != nil {
			return shelveMsg{seq: seq, step: stepPull, err: err}
		}
		if !ok {
			msg.skipped = true // not remote-tracking: merged as it stands, and said so
		} else if err := repo.FetchRef(ctx, remote, rb); err != nil {
			return shelveMsg{seq: seq, step: stepPull, err: err}
		}
		if mode != modeUpdate {
			return msg
		}

		// A branch with no upstream is not an error here: it means there is nothing
		// to pull into it, and stepPush is where that becomes a report.
		msg.upstreamRef, err = repo.Upstream(ctx, branch)
		if err != nil || msg.upstreamRef == "" {
			msg.err = err
			return msg
		}
		ur, ub, ok, err := repo.RemoteRef(ctx, msg.upstreamRef)
		if err != nil {
			return shelveMsg{seq: seq, step: stepPull, err: err}
		}
		if !ok {
			return msg
		}
		msg.pushRemote, msg.pushBranch = ur, ub
		if err := repo.FetchRef(ctx, ur, ub); err != nil {
			return shelveMsg{seq: seq, step: stepPull, err: err}
		}
		return msg
	}
}

// shelveHoldsCmd is the last read-only step: recompute how far the branch has
// drifted from the refs just fetched, then check the one hazard no mechanism can
// avoid — an incoming change to a file you hold on this machine.
//
// Drift does not rely on git's behavior for that case (it varies by version —
// abort vs. clobber), it checks first, and it checks *before* the stash so a halt
// leaves nothing to undo (docs/specs/local-only-changes.md). Both merges `u`
// performs are checked, not just the target's: the branch's own upstream can
// carry a change to a held file exactly as readily.
func shelveHoldsCmd(ctx context.Context, repo *git.Repo, st store.Store, seq int, mode shelveMode, branch, targetRef, upstreamRef string) tea.Cmd {
	return func() tea.Msg {
		msg := shelveMsg{seq: seq, step: stepHolds}
		ab, err := repo.AheadBehind(ctx, branch, targetRef)
		if err != nil {
			return shelveMsg{seq: seq, step: stepHolds, err: err}
		}
		msg.behind = ab.Behind

		var incomingFrom []string
		if ab.Behind > 0 {
			incomingFrom = append(incomingFrom, targetRef)
		}
		if mode == modeUpdate && upstreamRef != "" {
			up, err := repo.AheadBehind(ctx, branch, upstreamRef)
			if err != nil {
				return shelveMsg{seq: seq, step: stepHolds, err: err}
			}
			msg.upBehind, msg.upAhead = up.Behind, up.Ahead
			if up.Behind > 0 {
				incomingFrom = append(incomingFrom, upstreamRef)
			}
		}
		if len(incomingFrom) == 0 {
			return msg
		}

		held, err := heldSet(ctx, repo)
		if err != nil {
			return shelveMsg{seq: seq, step: stepHolds, err: err}
		}
		if len(held) == 0 {
			return msg
		}

		seen := make(map[string]bool)
		for _, ref := range incomingFrom {
			incoming, err := repo.ChangedFiles(ctx, branch, ref)
			if err != nil {
				return shelveMsg{seq: seq, step: stepHolds, err: err}
			}
			for _, p := range incoming { // incoming order, so the report is deterministic
				if held[p] && !seen[p] {
					seen[p] = true
					msg.files = append(msg.files, shelveFile{path: p, note: st.LocalOnlyNote(p)})
				}
			}
		}
		return msg
	}
}

// heldSet is what this machine keeps back from commits. Git's own flags are the
// source of truth, exactly as on the local-only screen: the skip-worktree bit
// for tracked paths, Drift's fenced block in info/exclude for untracked ones.
// Read fresh, never assumed from the store, which holds only the notes.
func heldSet(ctx context.Context, repo *git.Repo) (map[string]bool, error) {
	skipped, err := repo.SkipWorktreeFiles(ctx)
	if err != nil {
		return nil, err
	}
	excluded, err := repo.ExcludedPaths(ctx)
	if err != nil {
		return nil, err
	}
	held := toSet(skipped)
	for _, p := range excluded {
		held[p] = true
	}
	return held, nil
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

// shelveSwitchCmd checks the branch out — the step ADR 0002 exists for. A
// refusal here is git protecting something the stash could not see (a
// skip-worktree file that differs between the branches), so the sequence rolls
// back rather than forcing past it.
func shelveSwitchCmd(ctx context.Context, repo *git.Repo, seq int, branch string, plan unwindPlan) tea.Cmd {
	return func() tea.Msg {
		if err := repo.Checkout(ctx, branch); err != nil {
			return shelveMsg{seq: seq, step: stepSwitch, err: err, restored: unwind(ctx, repo, plan)}
		}
		return shelveMsg{seq: seq, step: stepSwitch}
	}
}

// shelveUpstreamCmd pulls the branch's own upstream — the half that keeps `u`
// honest on a second machine, where merging the target into a stale branch
// produces something that cannot be pushed. Normally a fast-forward; a conflict
// means the branch diverged from itself, which is a halt in its own right.
func shelveUpstreamCmd(ctx context.Context, repo *git.Repo, cfg store.Config, seq int, upstreamRef string, plan unwindPlan) tea.Cmd {
	return func() tea.Msg {
		if upstreamRef == "" {
			return shelveMsg{seq: seq, step: stepUpstream, skipped: true}
		}
		return mergeStep(ctx, repo, cfg, seq, stepUpstream, upstreamRef, plan)
	}
}

// shelveMergeCmd merges the target and, on a conflict, undoes the whole thing:
// abort the merge, go back, put the stash back, report what collided. The
// recovery lives inside this one Cmd on purpose — it is a single atomic "never
// mind", not two steps a user could be shown standing between.
func shelveMergeCmd(ctx context.Context, repo *git.Repo, cfg store.Config, seq int, targetRef string, plan unwindPlan) tea.Cmd {
	return func() tea.Msg {
		return mergeStep(ctx, repo, cfg, seq, stepMerge, targetRef, plan)
	}
}

// mergeStep is the shape both merges share: merge, and on anything other than
// success roll the whole sequence back before reporting. So the mutating half
// either lands whole or leaves no trace.
func mergeStep(ctx context.Context, repo *git.Repo, cfg store.Config, seq int, step shelveStep, ref string, plan unwindPlan) tea.Msg {
	conflicts, err := repo.Merge(ctx, ref)
	if err != nil {
		return shelveMsg{seq: seq, step: step, err: err, restored: unwind(ctx, repo, plan)}
	}
	if len(conflicts) == 0 {
		return shelveMsg{seq: seq, step: step}
	}
	return shelveMsg{seq: seq, step: step,
		files:    classify(ctx, repo, cfg, conflicts),
		restored: unwind(ctx, repo, plan),
	}
}

// shelvePushCmd publishes the branch — the step that makes "this branch is up to
// date" a claim about the remote and not just about this machine.
//
// A rejection is not an error and never a force: it means the branch moved on
// the remote after the pull read it, which is someone else's commit. The
// sequence carries on to put the user back, and the report says the publish is
// the one thing that did not happen.
func shelvePushCmd(ctx context.Context, repo *git.Repo, seq int, remote, local, remoteBranch string, plan unwindPlan) tea.Cmd {
	return func() tea.Msg {
		if remote == "" {
			return shelveMsg{seq: seq, step: stepPush, skipped: true}
		}
		outcome, err := repo.Push(ctx, remote, local, remoteBranch)
		if err != nil {
			return shelveMsg{seq: seq, step: stepPush, err: err, restored: unwind(ctx, repo, plan)}
		}
		return shelveMsg{seq: seq, step: stepPush, pushed: outcome, rejected: outcome == git.PushRejected}
	}
}

// shelveReturnCmd puts the user back on the branch they were standing on. It
// deliberately does not pop on failure: a stash belongs to the branch it was
// taken on, and popping it here — wherever "here" turned out to be — is the one
// thing this whole arrangement exists to prevent.
func shelveReturnCmd(ctx context.Context, repo *git.Repo, seq int, back string) tea.Cmd {
	return func() tea.Msg {
		if err := repo.Checkout(ctx, back); err != nil {
			return shelveMsg{seq: seq, step: stepReturn, err: err,
				next: "git switch " + back + ", then git stash pop"}
		}
		return shelveMsg{seq: seq, step: stepReturn}
	}
}

// shelveRestoreCmd puts the stashed work back, on the branch it was taken from.
// A conflict here is the destination, not a failure: git retains the stash entry
// when a pop conflicts, so the work is still safe and the user is standing
// exactly where the hand reconciliation happens.
func shelveRestoreCmd(ctx context.Context, repo *git.Repo, cfg store.Config, seq int, stashOID string) tea.Cmd {
	return func() tea.Msg {
		conflicts, err := repo.StashPop(ctx, stashOID)
		if err != nil {
			return shelveMsg{seq: seq, step: stepRestore, err: err,
				next: "git stash list — your work is still stashed"}
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
	title := "Shelve " + s.targetKey + " onto " + s.branch
	sub := s.targetRef + " → " + s.branch + "   (" + s.ticketID + ")"
	if s.mode == modeUpdate {
		title = "Update " + s.branch + " from " + s.targetKey
		if s.leaves() {
			sub += "   · started on " + s.from
		}
	}

	if s.confirm {
		return m.screenView(m.confirmPlanBody(title), m.shelveHelp())
	}

	lines := []string{
		m.styles.hint.Render(title),
		m.styles.help.Render("  " + sub),
		"",
	}
	for _, step := range s.steps() {
		lines = append(lines, m.shelveStepRow(step.step, step.what))
	}
	if body := m.shelveOutcomeBody(); body != "" {
		lines = append(lines, "", body)
	}
	return m.screenView(strings.Join(lines, "\n"), m.shelveHelp())
}

// confirmPlanBody is the plan overlay (roadmap 17b, widened by 19a), drawn in the
// panel's place — the same mechanism as the declare overlay and the target
// picker.
//
// It names the plan in the order the sequence will run it, in the user's own
// terms, and it names the two things nothing else on screen says out loud: the
// **ref** being merged, and where the push lands. Both are the point. The
// dashboard shows a target's *key*, which in the v0.3.0 incident read `mvp-3`
// and was correct — the ref behind it was somebody's ticket branch, and one
// keypress published a merge of it. An overlay reading `merge in mvp-3` would
// have reprinted the lie; one reading the ref stops it dead, for free, before
// anything is published.
//
// Being blocked is the friction `u` exists to remove, so this is deliberately a
// prompt and not a refusal — but a prompt that says no more than "are you sure?"
// would be the same surprise with an extra keystroke.
//
// The guarantee at the foot answers the question actually being asked when there
// is work on the tree, which is not "will this work" but "where does my work end
// up": it is stashed on the branch it was taken from and popped back on that same
// branch, on every path including every halt. That is the invariant ADR 0002 kept
// when it traded away "Drift never checks anything out", and this is the one
// screen where the user has to take it on trust before it happens.
//
// Every line is clipped rather than wrapped, for the reason the help overlay's
// are: this panel's height is budgeted in lines, and prose that wraps spends
// lines the budget never costed (DESIGN.md §1).
func (m Model) confirmPlanBody(title string) string {
	s := m.shelve
	lines := []string{m.styles.hint.Render(title), ""}
	if s.dirty {
		what := "  ● " + s.from + " has uncommitted work."
		if s.leaves() {
			what = "  ● " + s.from + " has uncommitted work, and " + s.branch + " isn't checked out."
		}
		lines = append(lines, m.styles.dirty.Render(what), "")
	}

	lines = append(lines, m.styles.help.Render("  Drift will:"))
	n := 0
	plan := func(what string) {
		n++
		lines = append(lines, m.styles.help.Render(fmt.Sprintf("    %d.  %s", n, what)))
	}
	// A step whose variable half is a ref puts it *last*, so the bound below cuts
	// the ref's tail and never the words around it.
	planRef := func(verb, ref string) {
		plan(verb + " " + m.boundRef(fmt.Sprintf("    %d.  %s ", n+1, verb), ref))
	}

	if s.dirty {
		plan("stash your work on " + s.from)
	}
	if s.leaves() {
		plan("check out " + s.branch)
	}
	planRef("merge in", s.targetRef)
	switch {
	case s.planNoUpstream:
		// The third answer, kept distinct here exactly as it is on the row and at
		// the push: a branch that has never been published has nowhere to publish
		// to, and the sequence will say so rather than fail.
		plan("publish it — there is no upstream to publish to yet")
	case s.planUpstream != "":
		planRef("publish it to", s.planUpstream)
	default:
		plan("publish it to its upstream")
	}
	switch {
	case s.leaves() && s.dirty:
		plan("return to " + s.from + " and put your work back")
	case s.leaves():
		plan("return to " + s.from)
	case s.dirty:
		plan("put your work back")
	}

	if s.dirty {
		lines = append(lines, "",
			// Deliberately name-free, unlike the plan above it. A line that
			// interpolated the branch twice measured 79 cells into a 76-cell panel at
			// the ordinary 80-column terminal, and clipping cut the sentence that
			// carries the guarantee mid-word. What is left is bounded, so the point
			// lands whatever the branches are called and however narrow the terminal
			// gets.
			m.styles.help.Render("  Your work is stashed and popped on the same branch — it never"),
			m.styles.help.Render("  crosses a boundary, and every halt unwinds the same way."))
	}
	lines = append(lines, "", m.styles.hint.Render("  "+planQuestion(s.dirty)+"  (y/n)"))

	for i, l := range lines {
		lines[i] = clipPanelLine(m.styles, m.width, l)
	}
	return strings.Join(lines, "\n")
}

// planQuestion is the overlay's last line, which names the thing the answer is
// actually about. With work on the tree that is the stash; without it, the run.
// It is the same wording as the help line's y, so the two cannot disagree.
func planQuestion(dirty bool) string {
	if dirty {
		return "Stash it and go?"
	}
	return "Run it?"
}

// boundRef renders a ref at the end of a plan line, bounded to what the line has
// left so a long one ellipsises at its **tail** rather than being cut blind.
//
// The end that goes is the decision, not a detail. `origin/fix/PSOT-22114-…` is
// what gives a wrong target away; the trailing `/mvp-3` is the part that made it
// look right in the first place. So this must never become a middle-elide that
// shows `origin/…/mvp-3` and hides the one half worth reading — which is exactly
// what someone "improving" it would reach for.
//
// Before the first WindowSizeMsg the width is unknown, and nothing is bounded
// against a guess: the ref goes out whole and clipPanelLine is the backstop.
func (m Model) boundRef(prefix, ref string) string {
	avail := contentWidth(m.styles, m.width) - lipgloss.Width(prefix)
	if avail <= 0 {
		return ref
	}
	return strings.TrimRight(fit(ref, avail), " ")
}

// shelveStepRow draws one step with its state. The steps before the stash are
// read-only, so a sequence that stops in that half has visibly touched nothing —
// which is the reassurance the ordering was designed to give.
//
// Three states a step can be in that its *position* does not reveal, and each is
// a claim the list would otherwise get wrong. A step with nothing to do says so
// rather than sitting unticked forever, since "pending" and "not needed" are the
// two things a stopped sequence must never conflate. A step that did not do its
// job is flagged where it sits, because the sequence may well have carried on
// past it — a rejected push is the case, and ticking it would be the report
// contradicting its own headline. And everything else that was reached did work,
// including the last one, so it gets its tick even when the run as a whole is a
// handoff.
func (m Model) shelveStepRow(step shelveStep, what string) string {
	s := m.shelve
	done := "  " + m.styles.sync.Render("✓") + "  " + m.styles.help.Render(what)
	switch {
	case s.skipped[step]:
		return "  " + m.styles.help.Render("–  "+what+" (not needed)")
	case s.problem[step] && s.outcome == shelveStopped:
		return "  " + m.styles.errText.Render("✗") + "  " + what
	case s.problem[step]:
		return "  " + m.styles.unmerge.Render("■") + "  " + what
	case step < s.step:
		return done
	case step > s.step:
		return "  " + m.styles.help.Render("·  "+what)
	case s.active:
		return "  " + m.spin.View() + " " + what
	default:
		return done
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

	lines := []string{m.shelveHead()}
	if s.reason != "" {
		lines = append(lines, m.styles.help.Render("  "+s.reason))
	}
	// The publish note rides alongside whatever else happened rather than
	// replacing it: a pop conflict is still the reconciliation point even when the
	// branch also failed to reach the remote, and the user needs both facts.
	if s.publish != "" && s.reason != "" {
		lines = append(lines, m.styles.help.Render("  "+s.publish))
	}
	for _, f := range s.files {
		lines = append(lines, "  "+m.shelveFileRow(f))
	}
	if s.next != "" {
		lines = append(lines, "", m.styles.hint.Render("  next: "+s.next))
	}
	return strings.Join(lines, "\n")
}

// shelveHead is the outcome's one-line headline, which is where the two verbs
// read differently: `s` reports a merge, `u` reports a branch that is up to date
// and published — or, when the push is the part that did not happen, one that is
// not.
func (m Model) shelveHead() string {
	s := m.shelve
	switch s.outcome {
	case shelveLanded:
		switch {
		case s.mode == modeShelve:
			return m.styles.sync.Render("✓ " + s.targetKey + " merged in, your work is back")
		case s.pushed == git.PushUpToDate:
			return m.styles.sync.Render("✓ " + s.branch + " is up to date — there was nothing new to publish")
		default:
			return m.styles.sync.Render("✓ " + s.branch + " is up to date and published")
		}
	case shelveCurrent:
		if s.mode == modeUpdate {
			return m.styles.sync.Render("✓ already up to date, here and on the remote. Nothing was touched")
		}
		return m.styles.sync.Render("✓ already current — " + s.targetKey + " hasn't moved. Nothing was touched")
	case shelveHeld:
		return m.styles.unmerge.Render("■ stopped before touching anything")
	case shelveReverted:
		return m.styles.unmerge.Render("■ rolled back — you are exactly where you started")
	case shelveHandoff:
		if s.reason == "" && s.publish != "" {
			return m.styles.unmerge.Render("■ merged locally, but " + s.publish)
		}
		return m.styles.unmerge.Render("■ merged, but your work needs reconciling by hand")
	case shelveStopped:
		return m.styles.errText.Render("✗ stopped")
	}
	return ""
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
	if m.shelve.confirm {
		// The two answers are the whole contract while the prompt is up, so they are
		// what the line must never stop saying — the tail, paid for first (chrome.go).
		yes := "y run it"
		if m.shelve.dirty {
			yes = "y stash and go"
		}
		return helpLine(m.styles, m.width, nil, []string{yes, "n cancel", "? help"})
	}
	if m.shelve.active && m.shelve.cancellable {
		return helpLine(m.styles, m.width, nil, []string{"esc cancel", "? help", "q quit"})
	}
	if m.shelve.active {
		return helpLine(m.styles, m.width,
			[]string{"running — it stops on its own"}, []string{"? help", "q quit"})
	}
	return helpLine(m.styles, m.width, nil, []string{"esc back", "? help", "q quit"})
}
