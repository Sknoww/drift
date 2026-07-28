package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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

	for _, s := range shelveSteps {
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
	next, cmd := m.beginShelve()
	m = next.(Model)
	for i := 0; m.shelve.active && cmd != nil; i++ {
		if i > len(shelveSteps) {
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
	m := New(git.New(dir), cfg, st)
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

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
