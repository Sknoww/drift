package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Sknoww/drift/internal/git"
	"github.com/Sknoww/drift/internal/store"
)

// The shelve sequence's decisions all live in Update, over plain data, which is
// what makes them testable without a repo: these drive the chain by handing
// applyShelve the step results a real run would produce. The git calls
// themselves are covered against real repos in internal/git.

// shelveModel puts the cursor on ABC-1's first branch row with that branch
// checked out — the one shape the sequence will run on.
func shelveModel() Model {
	m := newModel()
	m.current = "abc-1-perf"
	m.expanded["ABC-1"] = true
	m.cursor = 1
	return m
}

func begin(m Model) Model {
	next, _ := m.beginShelve()
	return next.(Model)
}

func step(m Model, msg shelveMsg) Model {
	msg.seq = m.shelve.seq
	next, _ := m.applyShelve(msg)
	return next.(Model)
}

// advanceToStash walks the read-only head of the chain: ready, pull, and a holds
// result with the target moved and nothing held colliding.
func advanceToStash(m Model) Model {
	m = step(m, shelveMsg{step: stepReady})
	m = step(m, shelveMsg{step: stepPull})
	return step(m, shelveMsg{step: stepHolds, behind: 2})
}

func TestDefaultShelveKeysAreEscOnly(t *testing.T) {
	k := DefaultShelveKeys()
	if got, ok := k.action("esc"); !ok || got != ActionCancel {
		t.Errorf("esc -> %q, %v; want %q", got, ok, ActionCancel)
	}
	// A report has nothing to move over and nothing to choose.
	for _, key := range []string{"j", "k", "enter", " "} {
		if _, ok := k.action(key); ok {
			t.Errorf("key %q is bound on the shelve report; it should have no meaning there", key)
		}
	}
}

func TestShelveRefusesABranchThatIsNotCheckedOut(t *testing.T) {
	// Drift never checks anything out: a stash belongs to the branch it was taken
	// on, so a cross-branch sequence would have to carry uncommitted work over a
	// branch boundary to put it back.
	m := shelveModel()
	m.current = "something-else"

	m = begin(m)

	if m.screen == screenShelve {
		t.Error("the sequence opened on a branch that is not checked out")
	}
	if !strings.Contains(m.notice, "git switch abc-1-perf") {
		t.Errorf("notice = %q, want it to name the command that fixes it", m.notice)
	}
}

func TestShelveRefusesOnDetachedHead(t *testing.T) {
	m := shelveModel()
	m.current = ""

	m = begin(m)

	if m.screen == screenShelve {
		t.Error("the sequence opened with a detached HEAD; there is nothing to merge into")
	}
	if !strings.Contains(m.notice, "detached HEAD") {
		t.Errorf("notice = %q, want it to say why", m.notice)
	}
}

func TestShelveRefusesATicketRow(t *testing.T) {
	m := shelveModel()
	m.cursor = 0 // the ticket headline

	m = begin(m)

	if m.screen == screenShelve {
		t.Error("the sequence opened on a ticket row; it runs per branch")
	}
}

func TestShelveRefusesAnUnknownTarget(t *testing.T) {
	m := shelveModel()
	m.store.Tickets[0].Branches[0].TargetKey = "gone"

	m = begin(m)

	if m.screen == screenShelve {
		t.Error("the sequence opened against a target that is not configured")
	}
	if !strings.Contains(m.notice, "unknown target") {
		t.Errorf("notice = %q, want it to name the problem", m.notice)
	}
}

func TestShelveRefusesASecondSequence(t *testing.T) {
	// One at a time: two concurrent stash/merge sequences against one index is
	// not a state worth being able to reach.
	m := begin(shelveModel())
	seq := m.shelve.seq
	m.screen = screenDashboard // as if the user backed out while it ran

	m = begin(m)

	if m.shelve.seq != seq {
		t.Error("a second sequence started while one was already running")
	}
	if !strings.Contains(m.notice, "already running") {
		t.Errorf("notice = %q, want it to say why", m.notice)
	}
}

func TestShelveStopsWhenTheTargetHasNotMoved(t *testing.T) {
	// behind is recomputed against the ref just pulled, before the stash — a
	// sequence that stashed first would churn the working tree to accomplish
	// nothing.
	m := begin(shelveModel())
	m = step(m, shelveMsg{step: stepReady})
	m = step(m, shelveMsg{step: stepPull})
	m = step(m, shelveMsg{step: stepHolds, behind: 0})

	if m.shelve.outcome != shelveCurrent {
		t.Errorf("outcome = %v, want shelveCurrent", m.shelve.outcome)
	}
	if m.shelve.active {
		t.Error("the sequence is still running after reporting nothing to do")
	}
	if m.shelve.step != stepHolds {
		t.Errorf("stopped at %v, want stepHolds — nothing past it should have run", m.shelve.step)
	}
}

func TestShelveHaltsOnAHeldCollisionBeforeTouchingAnything(t *testing.T) {
	// The one hazard no mechanism avoids: the target changed a file you hold on
	// this machine. Checked *before* the stash, so the halt leaves nothing to undo.
	m := begin(shelveModel())
	m = step(m, shelveMsg{step: stepReady})
	m = step(m, shelveMsg{step: stepPull})
	m = step(m, shelveMsg{step: stepHolds, behind: 3, files: []shelveFile{
		{path: "app.conf", note: "debug log level"},
	}})

	if m.shelve.outcome != shelveHeld {
		t.Fatalf("outcome = %v, want shelveHeld", m.shelve.outcome)
	}
	if m.shelve.step != stepHolds {
		t.Errorf("stopped at %v, want stepHolds — the stash must not have run", m.shelve.step)
	}
	if m.shelve.stashOID != "" {
		t.Error("a stash was taken before the collision check halted the sequence")
	}
	if m.shelve.next == "" {
		t.Error("the halt named no next action")
	}

	body := m.shelveOutcomeBody()
	if !strings.Contains(body, "app.conf") || !strings.Contains(body, "debug log level") {
		t.Errorf("report = %q, want the colliding path and its note", body)
	}
}

func TestShelveMergeConflictReportsARollback(t *testing.T) {
	// Abort-and-restore: the mutating half either lands whole or leaves no trace.
	m := advanceToStash(begin(shelveModel()))
	m = step(m, shelveMsg{step: stepStash, stashOID: "abc123"})
	m = step(m, shelveMsg{step: stepMerge, files: []shelveFile{
		{path: "Scene.unity", unmergeable: true},
	}})

	if m.shelve.outcome != shelveReverted {
		t.Fatalf("outcome = %v, want shelveReverted", m.shelve.outcome)
	}
	body := m.shelveOutcomeBody()
	if !strings.Contains(body, "where you started") {
		t.Errorf("report = %q, want it to say the repo was put back", body)
	}
	if !strings.Contains(body, "unmergeable") {
		t.Errorf("report = %q, want the unmergeable flag — it decides whether this is a text merge or an external tool", body)
	}
}

func TestShelveMergeConflictWithAFailedRestoreSaysSo(t *testing.T) {
	// A failed abort or pop is the one case Drift cannot dress up: the work is
	// still stashed, and the report has to point at it.
	m := advanceToStash(begin(shelveModel()))
	m = step(m, shelveMsg{step: stepStash, stashOID: "abc123"})
	m = step(m, shelveMsg{
		step:     stepMerge,
		files:    []shelveFile{{path: "shared.txt"}},
		restored: errors.New("could not restore"),
	})

	if m.shelve.outcome != shelveStopped {
		t.Fatalf("outcome = %v, want shelveStopped", m.shelve.outcome)
	}
	if !strings.Contains(m.shelve.next, "git stash list") {
		t.Errorf("next = %q, want it to point at the stash the work is still in", m.shelve.next)
	}
}

func TestShelvePopConflictHaltsInPlaceAndSaysTheStashIsKept(t *testing.T) {
	// Deliberately asymmetric with a merge conflict. git retains the stash entry
	// when a pop conflicts, so nothing is at risk — and this is the reconciliation
	// point the sequence was run to reach. Restoring would undo the one thing that
	// went right.
	m := advanceToStash(begin(shelveModel()))
	m = step(m, shelveMsg{step: stepStash, stashOID: "abc123"})
	m = step(m, shelveMsg{step: stepMerge})
	m = step(m, shelveMsg{step: stepRestore, files: []shelveFile{
		{path: "workflows/a.uwe", unmergeable: true},
	}})

	if m.shelve.outcome != shelveHandoff {
		t.Fatalf("outcome = %v, want shelveHandoff", m.shelve.outcome)
	}
	if !strings.Contains(m.shelve.next, "stash") {
		t.Errorf("next = %q, want it to say the work is still stashed", m.shelve.next)
	}
}

func TestShelveLandsClean(t *testing.T) {
	m := advanceToStash(begin(shelveModel()))
	m = step(m, shelveMsg{step: stepStash, stashOID: "abc123"})
	m = step(m, shelveMsg{step: stepMerge})
	m = step(m, shelveMsg{step: stepRestore})

	if m.shelve.outcome != shelveLanded {
		t.Fatalf("outcome = %v, want shelveLanded", m.shelve.outcome)
	}
	if m.shelve.active {
		t.Error("the sequence is still marked running after landing")
	}
}

func TestShelveSkipsTheRestoreWhenNothingWasStashed(t *testing.T) {
	// `git stash push` on a clean tree creates no entry, so there is nothing to
	// pop — and popping anyway would restore someone else's work.
	m := advanceToStash(begin(shelveModel()))
	m = step(m, shelveMsg{step: stepStash, stashOID: ""})
	m = step(m, shelveMsg{step: stepMerge})

	if m.shelve.outcome != shelveLanded {
		t.Fatalf("outcome = %v, want shelveLanded without a restore step", m.shelve.outcome)
	}
	if m.shelve.step == stepRestore {
		t.Error("the sequence tried to pop a stash it never took")
	}
}

func TestShelveDropsAStaleStep(t *testing.T) {
	// A step landing for a sequence the user already cancelled must not act.
	m := begin(shelveModel())
	stale := shelveMsg{seq: m.shelve.seq - 1, step: stepReady}

	next, _ := m.applyShelve(stale)
	got := next.(Model)

	if got.shelve.step != stepReady {
		t.Errorf("a stale step advanced the sequence to %v", got.shelve.step)
	}
}

func TestShelveErrorStopsTheSequence(t *testing.T) {
	m := begin(shelveModel())
	m = step(m, shelveMsg{step: stepReady, err: errors.New("a merge is already in progress")})

	if m.shelve.outcome != shelveStopped {
		t.Fatalf("outcome = %v, want shelveStopped", m.shelve.outcome)
	}
	if !strings.Contains(m.shelve.reason, "already in progress") {
		t.Errorf("reason = %q, want git's own answer carried through", m.shelve.reason)
	}
}

func TestShelveCancelOnlyWhileNothingHasBeenTouched(t *testing.T) {
	// esc kills a hung pull. Once the stash is taken there is no cancelling into
	// an undefined middle, so it is refused rather than obeyed.
	m := begin(shelveModel())
	m = step(m, shelveMsg{step: stepReady}) // now on stepPull, still read-only

	next, _ := m.dispatchShelve(ActionCancel)
	canceled := next.(Model)
	if canceled.screen != screenDashboard {
		t.Error("esc during the pull did not hand control back")
	}
	if !strings.Contains(canceled.notice, "nothing was touched") {
		t.Errorf("notice = %q, want the reassurance that nothing was mutated", canceled.notice)
	}

	m = advanceToStash(begin(shelveModel()))
	m = step(m, shelveMsg{step: stepStash, stashOID: "abc123"})

	next, _ = m.dispatchShelve(ActionCancel)
	refused := next.(Model)
	if refused.screen != screenShelve {
		t.Error("esc mid-merge left the screen; the sequence must run to a halt")
	}
	if refused.shelve.active != true {
		t.Error("esc mid-merge stopped the sequence")
	}
}

func TestShelveEscFromTheReportReturnsHome(t *testing.T) {
	m := begin(shelveModel())
	m = step(m, shelveMsg{step: stepReady})
	m = step(m, shelveMsg{step: stepPull})
	m = step(m, shelveMsg{step: stepHolds, behind: 0})

	next, _ := m.dispatchShelve(ActionCancel)
	if got := next.(Model); got.screen != screenDashboard {
		t.Error("esc on a finished report did not return to the dashboard")
	}
}

func TestShelveViewShowsEveryStep(t *testing.T) {
	// The sequence mutates the working tree; a single undifferentiated spinner
	// would give the user nothing to reason about when it stops.
	m := begin(shelveModel())
	m.width = 100
	view := m.shelveView()

	for _, s := range m.shelve.steps() {
		if !strings.Contains(view, s.what) {
			t.Errorf("view is missing the %q step", s.what)
		}
	}
	if !strings.Contains(view, "abc-1-perf") || !strings.Contains(view, "origin/release-to-performance") {
		t.Errorf("view = %q, want the branch and the target ref it is merging", view)
	}
}

func TestShelveIsReachableFromTheDashboardKeymap(t *testing.T) {
	if got, ok := DefaultDashboardKeys().action("s"); !ok || got != ActionShelve {
		t.Errorf("dashboard key s -> %q, %v; want %q", got, ok, ActionShelve)
	}
	if _, ok := actionText[ActionShelve]; !ok {
		t.Error("ActionShelve has no wording, so the generated ? table would show its raw name")
	}
}

// A running sequence must keep the spinner alive on its own account: it is not a
// status sweep, so m.loading is false throughout.
func TestSpinnerKeepsTickingDuringAShelve(t *testing.T) {
	m := begin(shelveModel())
	if m.loading {
		t.Fatal("the shelve set the sweep's loading flag; the two are different things")
	}
	_, cmd := m.Update(m.spin.Tick())
	if cmd == nil {
		t.Error("the spinner stopped ticking while a shelve was running")
	}
}

var _ tea.Model = Model{}

// --- the whole chain, against a real repo ---------------------------------

// runChain drives the sequence for real: each Cmd is executed and its message
// fed back through applyShelve, exactly as Bubble Tea would, until the sequence
// ends. What the state-machine tests above prove over synthetic messages, this
// proves over messages git actually produced.
func runChain(t *testing.T, m Model) Model {
	t.Helper()
	return runChainAs(t, m, modeShelve)
}

// runChainAs is runChain for either verb, so the update sequence gets the same
// treatment: driven end to end against a real repo, not just over synthetic
// messages.
func runChainAs(t *testing.T, m Model, mode shelveMode) Model {
	t.Helper()
	var next tea.Model
	var cmd tea.Cmd
	if mode == modeUpdate {
		next, cmd = m.beginUpdate()
	} else {
		next, cmd = m.beginShelve()
	}
	return driveChain(t, next.(Model), cmd)
}

// driveChain runs the Cmd chain until it stops. It ends of its own accord where
// the sequence stops for input — the stash prompt fires no Cmd — so a test that
// wants the answered path has to say so, which is the point: the prompt is a
// halt, and a driver that answered it silently would be testing a sequence
// nobody agreed to.
func driveChain(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	var next tea.Model
	for i := 0; m.shelve.active && cmd != nil; i++ {
		if i > len(m.shelve.steps()) {
			t.Fatal("the sequence did not terminate")
		}
		sm, ok := shelveResult(cmd)
		if !ok {
			t.Fatal("step produced no shelveMsg")
		}
		next, cmd = m.applyShelve(sm)
		m = next.(Model)
	}
	return m
}

// answerStashPrompt presses y and runs the rest of the chain, the way a user who
// agreed to the plan does.
func answerStashPrompt(t *testing.T, m Model) Model {
	t.Helper()
	if !m.shelve.confirm {
		t.Fatal("expected the sequence to be waiting on the stash prompt")
	}
	next, cmd := m.dispatch(ActionConfirm)
	return driveChain(t, next.(Model), cmd)
}

// shelveResult runs a Cmd and digs the sequence's own message out of it. The
// first one is batched with the spinner tick, so a plain type assertion would
// only ever see the batch.
func shelveResult(cmd tea.Cmd) (shelveMsg, bool) {
	switch msg := cmd().(type) {
	case shelveMsg:
		return msg, true
	case tea.BatchMsg:
		for _, c := range msg {
			if got, ok := shelveResult(c); ok {
				return got, true
			}
		}
	}
	return shelveMsg{}, false
}

// shelveRepoModel wires a model to a real repo whose "target" is a local branch
// that has moved ahead of the checked-out feature branch. A local ref means the
// pull step reports itself unfetchable and merges the ref as it stands — which is
// the behavior worth having under test on its own.
func shelveRepoModel(t *testing.T, dir string) Model {
	t.Helper()
	cfg := store.Config{Targets: []store.Target{{Key: "main", Ref: "main"}}}
	st := store.Store{Tickets: []store.Ticket{{ID: "ABC-1", Branches: []store.TicketBranch{
		{Branch: "feature", TargetKey: "main"},
	}}}}
	m := New(git.New(dir), cfg, st, store.Prefs{})
	m.loading = false
	m.current = "feature"
	m.expanded["ABC-1"] = true
	m.cursor = 1
	return m
}

func TestShelveEndToEndCarriesLocalOnlyChangesThrough(t *testing.T) {
	// The whole promise in one run: the target moved, you have real uncommitted
	// work *and* a held local-only edit, and one keypress lands the merge with
	// both sides intact and no re-apply step.
	dir := newTestRepo(t)
	writeCommit(t, dir, "app.conf", "level=info\n")
	rungit(t, dir, "checkout", "--quiet", "-b", "feature")

	// main moves underneath.
	rungit(t, dir, "checkout", "--quiet", "main")
	writeCommit(t, dir, "incoming.txt", "from the target\n")
	rungit(t, dir, "checkout", "--quiet", "feature")

	// A held tracked edit, plus ordinary work the stash is actually for.
	if err := os.WriteFile(filepath.Join(dir, "app.conf"), []byte("level=debug\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := git.New(dir).SetSkipWorktree(context.Background(), "app.conf"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("my work\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := runChain(t, shelveRepoModel(t, dir))

	if m.shelve.outcome != shelveLanded {
		t.Fatalf("outcome = %v (%s), want shelveLanded", m.shelve.outcome, m.shelve.reason)
	}
	if got := readFile(t, dir, "app.conf"); got != "level=debug\n" {
		t.Errorf("held edit after the sequence = %q, want it carried through untouched", got)
	}
	if got := readFile(t, dir, "seed.txt"); got != "my work\n" {
		t.Errorf("uncommitted work after the sequence = %q, want it restored", got)
	}
	if got := readFile(t, dir, "incoming.txt"); got != "from the target\n" {
		t.Errorf("the target's commit did not land: %q", got)
	}
}

func TestShelveEndToEndHaltsOnAHeldCollision(t *testing.T) {
	// The target changed the very file you hold locally. Drift must catch that
	// before the merge — and, because the check runs before the stash, leave the
	// working tree exactly as it found it.
	dir := newTestRepo(t)
	writeCommit(t, dir, "app.conf", "level=info\n")
	rungit(t, dir, "checkout", "--quiet", "-b", "feature")

	rungit(t, dir, "checkout", "--quiet", "main")
	if err := os.WriteFile(filepath.Join(dir, "app.conf"), []byte("level=warn\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rungit(t, dir, "commit", "--quiet", "-am", "target touches the held file")
	rungit(t, dir, "checkout", "--quiet", "feature")

	if err := os.WriteFile(filepath.Join(dir, "app.conf"), []byte("level=debug\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := git.New(dir).SetSkipWorktree(context.Background(), "app.conf"); err != nil {
		t.Fatal(err)
	}

	m := shelveRepoModel(t, dir)
	m.store = m.store.SetLocalOnlyNote("app.conf", "debug log level")
	m = runChain(t, m)

	if m.shelve.outcome != shelveHeld {
		t.Fatalf("outcome = %v (%s), want shelveHeld", m.shelve.outcome, m.shelve.reason)
	}
	if m.shelve.step != stepHolds {
		t.Errorf("stopped at %v, want stepHolds — nothing should have been stashed", m.shelve.step)
	}
	if got := readFile(t, dir, "app.conf"); got != "level=debug\n" {
		t.Errorf("app.conf = %q, want the held edit untouched by the halt", got)
	}
	if got := len(m.shelve.files); got != 1 || m.shelve.files[0].path != "app.conf" {
		t.Fatalf("reported files = %+v, want just app.conf", m.shelve.files)
	}
	if m.shelve.files[0].note != "debug log level" {
		t.Errorf("note = %q, want the local-only annotation carried into the report", m.shelve.files[0].note)
	}
}

func TestShelveEndToEndRollsBackAMergeConflict(t *testing.T) {
	// Both sides committed to the same file. The sequence aborts the merge, puts
	// the stash back, and leaves the user byte-for-byte where they started.
	dir := newTestRepo(t)
	writeCommit(t, dir, "shared.txt", "base\n")
	rungit(t, dir, "checkout", "--quiet", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rungit(t, dir, "commit", "--quiet", "-am", "mine")

	rungit(t, dir, "checkout", "--quiet", "main")
	if err := os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rungit(t, dir, "commit", "--quiet", "-am", "theirs")
	rungit(t, dir, "checkout", "--quiet", "feature")

	// Uncommitted work that must come back after the rollback.
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("my work\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := runChain(t, shelveRepoModel(t, dir))

	if m.shelve.outcome != shelveReverted {
		t.Fatalf("outcome = %v (%s), want shelveReverted", m.shelve.outcome, m.shelve.reason)
	}
	if got := readFile(t, dir, "shared.txt"); got != "mine\n" {
		t.Errorf("shared.txt = %q, want the branch's own version — the merge was meant to be undone", got)
	}
	if got := readFile(t, dir, "seed.txt"); got != "my work\n" {
		t.Errorf("uncommitted work = %q, want it restored by the rollback", got)
	}
	if op, _ := git.New(dir).OperationInProgress(context.Background()); op != "" {
		t.Errorf("left the repo mid-%s; the rollback must leave no trace", op)
	}
}

func TestShelveEndToEndHandsOffAPopConflict(t *testing.T) {
	// The flow CONTEXT.md describes, start to finish: the unmergeable file's local
	// edit is in the stash, so the *merge* is clean and the target's version lands
	// whole — the conflict surfaces on the way back, with no conflict markers ever
	// written into the file. Drift stops there, because that is the point at which
	// a human reconciles in an external tool.
	dir := newTestRepo(t)
	writeCommit(t, dir, "scene.unity", "base\n")
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("*.unity -merge\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rungit(t, dir, "add", ".gitattributes")
	rungit(t, dir, "commit", "--quiet", "-m", "declare unity unmergeable")
	rungit(t, dir, "checkout", "--quiet", "-b", "feature")

	rungit(t, dir, "checkout", "--quiet", "main")
	if err := os.WriteFile(filepath.Join(dir, "scene.unity"), []byte("the target's version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rungit(t, dir, "commit", "--quiet", "-am", "target edits the scene")
	rungit(t, dir, "checkout", "--quiet", "feature")

	// Uncommitted work on the same unmergeable file.
	if err := os.WriteFile(filepath.Join(dir, "scene.unity"), []byte("my uncommitted version\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := runChain(t, shelveRepoModel(t, dir))

	if m.shelve.outcome != shelveHandoff {
		t.Fatalf("outcome = %v (%s), want shelveHandoff", m.shelve.outcome, m.shelve.reason)
	}
	if got := len(m.shelve.files); got != 1 || m.shelve.files[0].path != "scene.unity" {
		t.Fatalf("reported files = %+v, want just scene.unity", m.shelve.files)
	}
	if !m.shelve.files[0].unmergeable {
		t.Error("scene.unity was not flagged unmergeable, though .gitattributes declares it")
	}
	if got := readFile(t, dir, "scene.unity"); strings.Contains(got, "<<<<<<<") {
		t.Errorf("conflict markers were written into an unmergeable file:\n%s", got)
	}
	// The halt is only safe because the work is still stashed. Say so, and mean it.
	oid, err := git.New(dir).StashRef(context.Background())
	if err != nil || oid == "" {
		t.Errorf("StashRef() = %q, %v; want the entry retained after a conflicted pop", oid, err)
	}
	if !strings.Contains(m.shelve.next, "stash") {
		t.Errorf("next = %q, want it to tell the user the work is still stashed", m.shelve.next)
	}
}

// --- area 17a: the update sequence ----------------------------------------

// updateModel puts the cursor on ABC-1's branch row while a *different* branch
// is checked out — the shape `s` refuses and `u` exists for.
func updateModel() Model {
	m := shelveModel()
	m.current = "somewhere-else"
	return m
}

func beginUpdateOn(m Model) Model {
	next, _ := m.beginUpdate()
	return next.(Model)
}

func TestUpdateRunsOnABranchThatIsNotCheckedOut(t *testing.T) {
	// The finding that raised the area: going down the list pressing `s` refuses
	// on every row but the one you happen to be standing on, so the "one keypress
	// per branch" payoff only ever arrives for one branch.
	m := beginUpdateOn(updateModel())

	if !m.shelve.active {
		t.Fatalf("u refused a branch that is not checked out: %q", m.notice)
	}
	if !m.shelve.leaves() {
		t.Error("the sequence does not know it has to leave, so it owes no return")
	}
	if m.shelve.from != "somewhere-else" {
		t.Errorf("from = %q, want the branch the user was standing on", m.shelve.from)
	}
}

func TestShelveStillRefusesAnotherBranchAndPointsAtUpdate(t *testing.T) {
	// `s` keeps its scope exactly as it shipped — a verb that has shipped is a
	// verb someone has — but the refusal now names the verb that does the job.
	next, _ := updateModel().beginShelve()
	m := next.(Model)

	if m.shelve.active {
		t.Fatal("s ran on a branch that is not checked out")
	}
	if !strings.Contains(m.notice, "press u") {
		t.Errorf("notice = %q, want it to point at the verb that handles this", m.notice)
	}
}

func TestUpdateAsksBeforeStashingWorkOnTheBranchItLeaves(t *testing.T) {
	// The one case `u` asks about. Being blocked by unrelated dirt is the friction
	// the verb exists to remove, so this is a prompt and not a refusal — but being
	// stashed without having agreed to it is the surprise the prompt exists to
	// prevent. It is taken at stepReady, while everything is still read-only.
	m := beginUpdateOn(updateModel())
	m = step(m, shelveMsg{step: stepReady, dirty: true})

	if !m.shelve.confirm {
		t.Fatal("u stashed work on the branch it is leaving without asking")
	}
	if !m.shelve.active || m.shelve.outcome != shelveRunning {
		t.Error("the prompt ended the sequence; it is a question, not a refusal")
	}
	if m.shelve.step != stepReady {
		t.Errorf("asked at %v, want stepReady — nothing may have been touched yet", m.shelve.step)
	}
	if !m.shelve.cancellable {
		t.Error("declining must still be a cancel that has nothing to undo")
	}
}

func TestStashPromptNamesThePlanRatherThanAskingAreYouSure(t *testing.T) {
	// A prompt that says no more than "are you sure?" is the same surprise with an
	// extra keystroke. It has to name which branch is being left, that the work is
	// stashed, and that Drift comes back and puts it back.
	m := beginUpdateOn(updateModel())
	m.width, m.height = 100, 40
	m = step(m, shelveMsg{step: stepReady, dirty: true})

	view := m.View()
	for _, want := range []string{
		"somewhere-else", // the branch being left, named
		"abc-1-perf",     // the branch being updated
		"stash your work",
		"return to",
		"(y/n)",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the prompt never says %q:\n%s", want, view)
		}
	}
	// The step list is what the prompt replaces — an overlay is drawn in the
	// panel's place, the same mechanism as the declare overlay.
	if strings.Contains(view, "check the repo is ready") {
		t.Error("the prompt was drawn over the step list instead of in its place")
	}
}

func TestDecliningTheStashPromptTouchesNothing(t *testing.T) {
	// The sequence is still on its read-only head, so declining is exactly the
	// cancel the screen already had — down to the notice saying so.
	m := beginUpdateOn(updateModel())
	m = step(m, shelveMsg{step: stepReady, dirty: true})

	next, _ := m.dispatch(ActionCancel)
	m = next.(Model)

	if m.shelve.active || m.shelve.confirm {
		t.Fatal("declining left the sequence running")
	}
	if m.screen != screenDashboard {
		t.Errorf("screen = %v, want the dashboard back", m.screen)
	}
	if !strings.Contains(m.notice, "nothing was touched") {
		t.Errorf("notice = %q, want it to say nothing was touched", m.notice)
	}
}

func TestAcceptingTheStashPromptRunsTheSameSequenceAsEveryOtherPath(t *testing.T) {
	// The confirmation gates *whether* the sequence runs, never how: it resumes at
	// the fetches, exactly where a sequence that needed no permission goes.
	m := beginUpdateOn(updateModel())
	m = step(m, shelveMsg{step: stepReady, dirty: true})

	next, cmd := m.dispatch(ActionConfirm)
	m = next.(Model)

	if m.shelve.confirm {
		t.Fatal("the prompt stayed open after it was answered")
	}
	if m.shelve.step != stepPull {
		t.Errorf("step = %v, want stepPull — the fetches are what comes next", m.shelve.step)
	}
	if cmd == nil {
		t.Error("nothing was fired; the sequence agreed to must actually run")
	}
}

func TestNoStashPromptWhenThereIsNothingToWarnAbout(t *testing.T) {
	// Two of the three cases need no prompt at all, and conflating them with the
	// third is what made cross-branch work look harder than it is.
	tests := []struct {
		name  string
		model Model
		dirty bool
	}{
		{"clean tree, crossing a branch boundary", updateModel(), false},
		{"dirty tree, but no boundary to cross", shelveModel(), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := beginUpdateOn(tc.model)
			m = step(m, shelveMsg{step: stepReady, dirty: tc.dirty})

			if m.shelve.confirm {
				t.Fatal("asked permission for something there is nothing to warn about")
			}
			if m.shelve.step != stepPull {
				t.Errorf("step = %v, want the sequence to have carried straight on", m.shelve.step)
			}
		})
	}
}

func TestHelpOverTheStashPromptCannotAnswerIt(t *testing.T) {
	// "Any key closes it, and is consumed" is what stops a key pressed to dismiss
	// the help from also acting on the screen underneath — and there is no screen
	// where that matters more than a y/n whose y stashes your work.
	m := beginUpdateOn(updateModel())
	m = step(m, shelveMsg{step: stepReady, dirty: true})

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = next.(Model)
	if !m.showHelp {
		t.Fatal("? did not open the help over the prompt")
	}
	if !strings.Contains(m.View(), "stash confirmation") {
		t.Errorf("the help names the wrong screen:\n%s", m.View())
	}

	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = next.(Model)
	if m.showHelp {
		t.Error("y did not close the help")
	}
	if !m.shelve.confirm || m.shelve.step != stepReady {
		t.Error("closing the help answered the prompt underneath it")
	}
}

func TestStashPromptFitsTheTerminalItIsDrawnInto(t *testing.T) {
	// Prose can break a frame exactly as rows can (DESIGN.md §1), and this screen
	// does not window — there is nothing to window around. So the overlay is
	// measured directly, with branch names long enough to be the thing that would
	// overflow if any line were left to wrap.
	m := beginUpdateOn(updateModel())
	m.shelve.from = "release/2024-q4-maintenance-and-then-some"
	m.shelve.branch = "feature/ABC-1234-a-name-nobody-would-type-twice"

	for _, size := range [][2]int{{minTerminalWidth, 24}, {80, 24}, {100, 30}} {
		m.width, m.height = size[0], size[1]
		next := m
		next = step(next, shelveMsg{step: stepReady, dirty: true})

		lines := strings.Split(next.View(), "\n")
		if len(lines) > size[1] {
			t.Errorf("%dx%d: the frame is %d lines and runs off the top", size[0], size[1], len(lines))
		}
		for _, l := range lines {
			if w := lipgloss.Width(l); w > size[0] {
				t.Errorf("%dx%d: a line is %d cells wide and wraps: %q", size[0], size[1], w, l)
			}
		}
	}
}

func TestStashPromptShadowsTheReportsKeymap(t *testing.T) {
	// An overlay is an overlay wherever the user meets one: while it is open the
	// keys are the delete confirmation's y/n shape, not the report's esc-only one.
	m := beginUpdateOn(updateModel())
	m = step(m, shelveMsg{step: stepReady, dirty: true})

	k := m.activeKeys()
	for key, want := range map[string]Action{
		"y": ActionConfirm, "enter": ActionConfirm,
		"n": ActionCancel, "esc": ActionCancel,
	} {
		if got, ok := k.action(key); !ok || got != want {
			t.Errorf("%q -> %q, %v; want %q", key, got, ok, want)
		}
	}
	// A y/n question's contract is y or n. A key that quietly means a third thing
	// is not part of it — the same reason the delete confirmation leaves q unbound.
	if _, ok := k.action("q"); ok {
		t.Error("q quits out from under a confirmation the user is still answering")
	}
	if _, ok := m.keys.shelve.action("y"); ok {
		t.Error("the report's own keymap grew a y; the overlay's keys must not leak into it")
	}
}

func TestUpdateOnTheCheckedOutBranchNeitherSwitchesNorReturns(t *testing.T) {
	// Same branch, dirty, is exactly today's shelve — no boundary is crossed. It
	// gains the upstream pull and the push and nothing else, which is why it needs
	// no new argument and no new prompt.
	m := beginUpdateOn(shelveModel())
	m = step(m, shelveMsg{step: stepReady, dirty: true})

	if m.shelve.leaves() {
		t.Fatal("the sequence thinks it has to leave a branch it is already on")
	}
	var what []string
	for _, s := range m.shelve.steps() {
		what = append(what, s.what)
	}
	joined := strings.Join(what, " · ")
	if strings.Contains(joined, "check out") || strings.Contains(joined, "return to") {
		t.Errorf("steps = %q, want no switch and no return when there is no boundary to cross", joined)
	}
	if !strings.Contains(joined, "publish") {
		t.Errorf("steps = %q, want the publish — it is what u adds over s", joined)
	}
}

func TestUpdateIsOnlyDoneWhenTheRemoteAgreesToo(t *testing.T) {
	// Half of "this branch is up to date" is a claim about the remote. A branch
	// level with its target but ahead of its own upstream still has work to do.
	m := beginUpdateOn(shelveModel())
	m = step(m, shelveMsg{step: stepReady})
	m = step(m, shelveMsg{step: stepPull, upstreamRef: "origin/abc-1-perf", pushRemote: "origin", pushBranch: "abc-1-perf"})
	m = step(m, shelveMsg{step: stepHolds, behind: 0, upBehind: 0, upAhead: 2})

	if m.shelve.outcome == shelveCurrent {
		t.Fatal("called an unpushed branch up to date — the push is the half that hadn't happened")
	}
	if m.shelve.step != stepStash {
		t.Errorf("step = %v, want the sequence to carry on to the push", m.shelve.step)
	}
}

func TestUpdateStopsWhenThereIsNothingLeftAnywhere(t *testing.T) {
	m := beginUpdateOn(shelveModel())
	m = step(m, shelveMsg{step: stepReady})
	m = step(m, shelveMsg{step: stepPull, upstreamRef: "origin/abc-1-perf", pushRemote: "origin", pushBranch: "abc-1-perf"})
	m = step(m, shelveMsg{step: stepHolds, behind: 0, upBehind: 0, upAhead: 0})

	if m.shelve.outcome != shelveCurrent {
		t.Fatalf("outcome = %v, want shelveCurrent", m.shelve.outcome)
	}
}

func TestUpdateNamesTheMissingUpstreamRatherThanClaimingSuccess(t *testing.T) {
	// A branch that has never been published has nothing to pull and nowhere to
	// push. Reporting it as up to date would be the one claim `u` must never make.
	m := beginUpdateOn(shelveModel())
	m = step(m, shelveMsg{step: stepReady})
	m = step(m, shelveMsg{step: stepPull}) // no upstream came back
	m = step(m, shelveMsg{step: stepHolds, behind: 0})

	if m.shelve.outcome != shelveStopped {
		t.Fatalf("outcome = %v, want shelveStopped", m.shelve.outcome)
	}
	if !strings.Contains(m.shelve.next, "push -u") {
		t.Errorf("next = %q, want the command that gives the branch an upstream", m.shelve.next)
	}
}

func TestUpdateRejectedPushIsAHandoffNotAFailure(t *testing.T) {
	// The rejection means someone else's commit is in the way — the class of thing
	// Drift stops and hands back. The branch is left updated and merged locally;
	// only the publish did not happen, and the report has to say exactly that.
	// (That git reports a rejection at all, rather than a bare non-zero exit, is
	// pinned against a real remote in internal/git.)
	m := beginUpdateOn(shelveModel())
	m = step(m, shelveMsg{step: stepReady})
	m = step(m, shelveMsg{step: stepPull, upstreamRef: "origin/abc-1-perf", pushRemote: "origin", pushBranch: "abc-1-perf"})
	m = step(m, shelveMsg{step: stepHolds, behind: 2})
	m = step(m, shelveMsg{step: stepStash, stashOID: "abc123"})
	m = step(m, shelveMsg{step: stepUpstream})
	m = step(m, shelveMsg{step: stepMerge})
	m = step(m, shelveMsg{step: stepPush, rejected: true})
	m = step(m, shelveMsg{step: stepRestore})

	if m.shelve.outcome != shelveHandoff {
		t.Fatalf("outcome = %v, want shelveHandoff — the merge landed, the publish didn't", m.shelve.outcome)
	}
	body := m.shelveOutcomeBody()
	if !strings.Contains(body, "rejected") {
		t.Errorf("report = %q, want the rejection named", body)
	}
	if !strings.Contains(body, "merged locally") {
		t.Errorf("report = %q, want it to say the merge did land", body)
	}

	// Found by rendering it: the step list flagged nothing and ticked the publish,
	// because it read the *last* step reached as the one that went wrong. Here the
	// sequence carried on past the failure deliberately, so the flag has to sit
	// where the failure was — and everything after it is entitled to its tick.
	if !strings.Contains(m.shelveStepRow(stepPush, "publish"), "■") {
		t.Error("the publish step is not flagged, so the list contradicts the headline")
	}
	if !strings.Contains(m.shelveStepRow(stepRestore, "put your work back"), "✓") {
		t.Error("the restore is flagged, though it is the push that failed")
	}
}

func TestUpdateIsReachableFromTheDashboardKeymap(t *testing.T) {
	if got, ok := DefaultDashboardKeys().action("u"); !ok || got != ActionUpdate {
		t.Errorf("dashboard key u -> %q, %v; want %q", got, ok, ActionUpdate)
	}
	// Two near-identical sequences are two entries in a generated help table, so
	// each description has to carry the distinction on its own. If they cannot,
	// the split is wrong.
	shelve, update := actionText[ActionShelve], actionText[ActionUpdate]
	if shelve == "" || update == "" {
		t.Fatal("one of the two verbs has no wording, so the ? table would show its raw name")
	}
	if !strings.Contains(shelve, "nothing is published") || !strings.Contains(update, "publish") {
		t.Errorf("shelve = %q, update = %q; want the commitment to be what tells them apart", shelve, update)
	}
}

// --- area 17a end to end, against a real repo -----------------------------

// updateOrigin builds a bare repo holding main and feature, the shape a clone
// can actually pull from and push back to.
func updateOrigin(t *testing.T) string {
	t.Helper()
	work := newTestRepo(t)
	writeCommit(t, work, "app.conf", "level=info\n")
	rungit(t, work, "branch", "feature")
	bare := t.TempDir()
	rungit(t, work, "init", "--quiet", "--bare", bare)
	rungit(t, work, "push", "--quiet", bare, "main", "feature")
	return bare
}

// updateClone checks main out with feature tracking origin/feature beside it —
// the branch the user is standing on is not the branch they are updating.
func updateClone(t *testing.T, origin string) string {
	t.Helper()
	dir := t.TempDir()
	rungit(t, dir, "clone", "--quiet", origin, dir)
	rungit(t, dir, "config", "user.email", "test@example.com")
	rungit(t, dir, "config", "user.name", "Test")
	rungit(t, dir, "branch", "--track", "feature", "origin/feature")
	return dir
}

// pushToOrigin commits to a branch through a throwaway second clone, which is
// how the remote moves under the repo being tested.
func pushToOrigin(t *testing.T, origin, branch, name, content string) {
	t.Helper()
	other := t.TempDir()
	rungit(t, other, "clone", "--quiet", "--branch", branch, origin, other)
	rungit(t, other, "config", "user.email", "test@example.com")
	rungit(t, other, "config", "user.name", "Test")
	writeCommit(t, other, name, content)
	rungit(t, other, "push", "--quiet", "origin", branch)
}

func updateRepoModel(t *testing.T, dir string) Model {
	t.Helper()
	cfg := store.Config{Targets: []store.Target{{Key: "main", Ref: "origin/main"}}}
	st := store.Store{Tickets: []store.Ticket{{ID: "ABC-1", Branches: []store.TicketBranch{
		{Branch: "feature", TargetKey: "main"},
	}}}}
	m := New(git.New(dir), cfg, st, store.Prefs{})
	m.loading = false
	m.current = "main"
	m.expanded["ABC-1"] = true
	m.cursor = 1
	return m
}

func TestUpdateEndToEndCarriesAnotherBranchAllTheWay(t *testing.T) {
	// The area in one run: standing on main, one keypress checks feature out,
	// pulls its own upstream, merges the target, publishes the result, and puts
	// you back on main — with a held local-only change carried across the branch
	// boundary untouched.
	origin := updateOrigin(t)
	dir := updateClone(t, origin)
	r := git.New(dir)
	ctx := context.Background()

	pushToOrigin(t, origin, "main", "incoming.txt", "from the target\n")
	pushToOrigin(t, origin, "feature", "theirs.txt", "from my other machine\n")

	// A local-only edit, held on this machine. A plain stash cannot see it, so it
	// has to ride the checkout as well as the merge.
	if err := os.WriteFile(filepath.Join(dir, "app.conf"), []byte("level=debug\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.SetSkipWorktree(ctx, "app.conf"); err != nil {
		t.Fatal(err)
	}

	m := runChainAs(t, updateRepoModel(t, dir), modeUpdate)

	if m.shelve.outcome != shelveLanded {
		t.Fatalf("outcome = %v (%s / %s), want shelveLanded", m.shelve.outcome, m.shelve.reason, m.shelve.publish)
	}
	if got, _ := r.CurrentBranch(ctx); got != "main" {
		t.Errorf("left the user on %q, want main — the list is a list, not a place you move to", got)
	}
	if got := readFile(t, dir, "app.conf"); got != "level=debug\n" {
		t.Errorf("held edit = %q, want it carried across the checkout untouched", got)
	}
	if ab, err := r.AheadBehind(ctx, "feature", "origin/main"); err != nil || ab.Behind != 0 {
		t.Errorf("feature vs origin/main = %+v, %v; want the target merged in", ab, err)
	}
	if ab, err := r.AheadBehind(ctx, "feature", "origin/feature"); err != nil || ab.Ahead != 0 || ab.Behind != 0 {
		t.Errorf("feature vs origin/feature = %+v, %v; want it pulled and published", ab, err)
	}
}

func TestUpdateEndToEndPutsYouBackWhenTheMergeConflicts(t *testing.T) {
	// "Every halt path unwinds it too" is the bulk of the work, and this is the
	// halt that proves it: the merge is rolled back, the checkout is undone, and
	// the user is standing where they started with nothing left behind.
	origin := updateOrigin(t)
	dir := updateClone(t, origin)
	r := git.New(dir)
	ctx := context.Background()

	pushToOrigin(t, origin, "main", "app.conf", "level=warn\n")
	pushToOrigin(t, origin, "feature", "app.conf", "level=trace\n")

	m := runChainAs(t, updateRepoModel(t, dir), modeUpdate)

	if m.shelve.outcome != shelveReverted {
		t.Fatalf("outcome = %v (%s), want shelveReverted", m.shelve.outcome, m.shelve.reason)
	}
	if got, _ := r.CurrentBranch(ctx); got != "main" {
		t.Errorf("a conflict left the user on %q, want main", got)
	}
	if op, _ := r.OperationInProgress(ctx); op != "" {
		t.Errorf("left the repo mid-%s; the rollback must leave no trace", op)
	}
	if ab, err := r.AheadBehind(ctx, "feature", "origin/main"); err != nil || ab.Behind == 0 {
		t.Errorf("feature vs origin/main = %+v, %v; want the merge undone", ab, err)
	}
}

func TestUpdateEndToEndWaitsOnTheDirtyTreeBeforeTouchingAnything(t *testing.T) {
	// The prompt, driven for real: the sequence stops at stepReady, so the working
	// tree is exactly as it was found and no stash exists to remember. Whatever the
	// user answers next, this much is true — which is what makes asking free.
	origin := updateOrigin(t)
	dir := updateClone(t, origin)
	pushToOrigin(t, origin, "main", "incoming.txt", "from the target\n")

	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("my work\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := runChainAs(t, updateRepoModel(t, dir), modeUpdate)

	if !m.shelve.confirm {
		t.Fatalf("outcome = %v (%s), want the sequence waiting on the prompt", m.shelve.outcome, m.shelve.reason)
	}
	if got := readFile(t, dir, "seed.txt"); got != "my work\n" {
		t.Errorf("seed.txt = %q, want the prompt to have touched nothing", got)
	}
	oid, err := git.New(dir).StashRef(context.Background())
	if err != nil || oid != "" {
		t.Errorf("StashRef() = %q, %v; want no stash — an unanswered prompt has nothing to undo", oid, err)
	}
	if got, _ := git.New(dir).CurrentBranch(context.Background()); got != "main" {
		t.Errorf("the prompt moved the user to %q", got)
	}
}

func TestUpdateEndToEndCarriesADirtyTreeAcrossTheBoundaryAndBack(t *testing.T) {
	// The case 17b unlocks, and the whole of what the prompt is agreeing to: work
	// is stashed on the branch being left, another branch is carried all the way,
	// and the work comes back on the branch it was taken from. The narrow claim —
	// a stash belongs to the branch it was taken on — is enforced by the return.
	origin := updateOrigin(t)
	dir := updateClone(t, origin)
	r := git.New(dir)
	ctx := context.Background()

	pushToOrigin(t, origin, "main", "incoming.txt", "from the target\n")

	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("my work\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := answerStashPrompt(t, runChainAs(t, updateRepoModel(t, dir), modeUpdate))

	if m.shelve.outcome != shelveLanded {
		t.Fatalf("outcome = %v (%s), want shelveLanded", m.shelve.outcome, m.shelve.reason)
	}
	if got, _ := r.CurrentBranch(ctx); got != "main" {
		t.Errorf("left the user on %q, want main — the list is not a place you move to", got)
	}
	if got := readFile(t, dir, "seed.txt"); got != "my work\n" {
		t.Errorf("seed.txt = %q, want the work back on the branch it was taken from", got)
	}
	if oid, err := r.StashRef(ctx); err != nil || oid != "" {
		t.Errorf("StashRef() = %q, %v; want the stash popped, not left behind", oid, err)
	}
	// feature was carried all the way: the target merged in, and the result on the
	// remote rather than only on this machine.
	if ab, err := r.AheadBehind(ctx, "feature", "origin/main"); err != nil || ab.Behind != 0 {
		t.Errorf("feature vs origin/main = %+v, %v; want the target merged in", ab, err)
	}
	if ab, err := r.AheadBehind(ctx, "feature", "origin/feature"); err != nil || ab.Ahead != 0 {
		t.Errorf("feature vs origin/feature = %+v, %v; want it published", ab, err)
	}
}

func TestUpdateEndToEndPublishesFromTheBranchYouAreOn(t *testing.T) {
	// The same-branch case: no boundary is crossed, so the sequence is today's
	// shelve plus the two halves that make the result leave the machine.
	origin := updateOrigin(t)
	dir := updateClone(t, origin)
	r := git.New(dir)
	ctx := context.Background()

	rungit(t, dir, "checkout", "--quiet", "feature")
	writeCommit(t, dir, "mine.txt", "mine\n")
	pushToOrigin(t, origin, "main", "incoming.txt", "from the target\n")

	m := updateRepoModel(t, dir)
	m.current = "feature"
	m = runChainAs(t, m, modeUpdate)

	if m.shelve.outcome != shelveLanded {
		t.Fatalf("outcome = %v (%s / %s), want shelveLanded", m.shelve.outcome, m.shelve.reason, m.shelve.publish)
	}
	if ab, err := r.AheadBehind(ctx, "feature", "origin/feature"); err != nil || ab.Ahead != 0 {
		t.Errorf("feature vs origin/feature = %+v, %v; want the commit published", ab, err)
	}
	if got, _ := r.CurrentBranch(ctx); got != "feature" {
		t.Errorf("ended on %q, want feature — it is where the user started", got)
	}
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
