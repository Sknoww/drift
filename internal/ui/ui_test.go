package ui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Sknoww/drift/internal/git"
	"github.com/Sknoww/drift/internal/store"
)

// TestMain drops git's system config so this package's real-repo tests see the
// same git on every machine. The full reasoning is in internal/git's TestMain;
// the short version is that a system gitconfig nobody in this repo wrote was
// deciding what branch `git init` created.
func TestMain(m *testing.M) {
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	os.Exit(m.Run())
}

// --- model fixtures -------------------------------------------------------

func sampleConfig() store.Config {
	return store.Config{Targets: []store.Target{
		{Key: "r2perf", Ref: "origin/release-to-performance"},
		{Key: "main", Ref: "origin/main"},
	}}
}

// samplePaths stands in for what store.LoadConfig resolved. Only the targets
// screen reads it, and only to say where a wrong target is corrected.
func samplePaths() store.Paths {
	return store.Paths{
		Dir:    "/repo/.git/drift",
		Config: "/repo/.git/drift/config.json",
		State:  "/repo/.git/drift/state.json",
	}
}

func sampleStore() store.Store {
	return store.Store{Tickets: []store.Ticket{
		{ID: "ABC-1", Title: "first", Branches: []store.TicketBranch{
			{Branch: "abc-1-perf", TargetKey: "r2perf"},
			{Branch: "abc-1-main", TargetKey: "main"},
		}},
		{ID: "ABC-2", Branches: []store.TicketBranch{
			{Branch: "abc-2", TargetKey: "main"},
		}},
		{ID: "ABC-3"},
	}}
}

// newModel builds a dashboard over a repo that is never dialed — the view and
// dispatch paths under test never shell out.
func newModel() Model {
	m := New(git.New(t_nowhere), samplePaths(), sampleConfig(), sampleStore(), store.Prefs{})
	m.loading = false // pretend the first sweep already landed
	return m
}

const t_nowhere = "/nonexistent-drift-test"

// --- named-action layer ---------------------------------------------------

func TestDefaultDashboardKeysCoverTable(t *testing.T) {
	k := DefaultDashboardKeys()
	want := map[string]Action{
		"j": ActionMoveDown, "k": ActionMoveUp,
		"enter": ActionToggleExpand, " ": ActionToggleExpand,
		"a": ActionAdd, "d": ActionDelete,
		"r": ActionRefresh, "f": ActionFetch, "esc": ActionCancel,
		"l": ActionLocalOnly, "s": ActionShelve,
		"q": ActionQuit, "ctrl+c": ActionQuit,
	}
	for key, action := range want {
		if got, ok := k.action(key); !ok || got != action {
			t.Errorf("key %q -> %q, %v; want %q", key, got, ok, action)
		}
	}
	if _, ok := k.action("z"); ok {
		t.Errorf("unbound key %q resolved to an action", "z")
	}
}

// --- dispatch -------------------------------------------------------------

func TestDispatchMovementClamps(t *testing.T) {
	m := newModel()

	// Up at the top is a no-op.
	next, _ := m.dispatch(ActionMoveUp)
	if got := next.(Model).cursor; got != 0 {
		t.Errorf("MoveUp at top: cursor = %d, want 0", got)
	}

	// Down walks to the last ticket and stops there.
	mm := m
	for i := 0; i < 10; i++ {
		next, _ := mm.dispatch(ActionMoveDown)
		mm = next.(Model)
	}
	if got, want := mm.cursor, len(m.store.Tickets)-1; got != want {
		t.Errorf("MoveDown past end: cursor = %d, want %d", got, want)
	}
}

func TestDispatchToggleExpand(t *testing.T) {
	m := newModel() // cursor on ABC-1
	next, _ := m.dispatch(ActionToggleExpand)
	if !next.(Model).expanded["ABC-1"] {
		t.Fatal("expected ABC-1 to expand")
	}
	next2, _ := next.(Model).dispatch(ActionToggleExpand)
	if next2.(Model).expanded["ABC-1"] {
		t.Fatal("expected ABC-1 to collapse")
	}
}

func TestDispatchQuit(t *testing.T) {
	m := newModel()
	_, cmd := m.dispatch(ActionQuit)
	if cmd == nil {
		t.Fatal("quit returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("quit command did not yield tea.QuitMsg")
	}
}

func TestDispatchLocalOnlyOpensTheManager(t *testing.T) {
	next, cmd := newModel().dispatch(ActionLocalOnly)
	m := next.(Model)
	if m.screen != screenLocalOnly {
		t.Errorf("screen = %v, want the local-only manager", m.screen)
	}
	// Nothing is cached between visits: the held set is asked of git every time,
	// since the flags can change outside Drift.
	if cmd == nil {
		t.Error("local-only: expected a command loading the held set")
	}
	if m.local.loaded {
		t.Error("local-only: opened already-loaded, so a stale set could be shown")
	}
}

// --- add flow -------------------------------------------------------------

// pairingModel drives the model to the pairing checklist for a fresh ticket ID
// with the given candidate branches already scanned in.
func pairingModel(t *testing.T, id string, branches ...string) Model {
	t.Helper()
	m := newModel()

	next, _ := m.dispatch(ActionAdd)
	m = next.(Model)
	if m.screen != screenAddID {
		t.Fatalf("ActionAdd: screen = %v, want screenAddID", m.screen)
	}

	m.input.SetValue(id)
	next, cmd := m.dispatch(ActionConfirm)
	m = next.(Model)
	if m.screen != screenPairing {
		t.Fatalf("confirm ID: screen = %v, want screenPairing", m.screen)
	}
	if cmd == nil {
		t.Fatal("confirm ID: expected a candidate-scan command")
	}
	return m.applyCandidates(candidatesMsg{id: id, branches: branches})
}

func TestAddIDRejectsEmptyAndDuplicate(t *testing.T) {
	m := newModel()
	next, _ := m.dispatch(ActionAdd)
	m = next.(Model)

	// Empty ID stays on the entry screen with a hint.
	m.input.SetValue("   ")
	next, _ = m.dispatch(ActionConfirm)
	if got := next.(Model); got.screen != screenAddID || got.notice == "" {
		t.Errorf("empty ID: screen=%v notice=%q, want stay + notice", got.screen, got.notice)
	}

	// An ID that already exists is refused.
	m.input.SetValue("ABC-1")
	next, _ = m.dispatch(ActionConfirm)
	if got := next.(Model); got.screen != screenAddID || !strings.Contains(got.notice, "already tracked") {
		t.Errorf("duplicate ID: screen=%v notice=%q, want stay + already-tracked", got.screen, got.notice)
	}
}

func TestApplyCandidatesDropsStaleScan(t *testing.T) {
	m := pairingModel(t, "NEW-9", "new-9-a")
	// A scan for a different ID (user moved on) must not overwrite the flow.
	got := m.applyCandidates(candidatesMsg{id: "OTHER-1", branches: []string{"x", "y"}})
	if len(got.add.candidates) != 1 || got.add.candidates[0].branch != "new-9-a" {
		t.Errorf("stale scan applied: %+v", got.add.candidates)
	}
}

func TestPairingAssignAndSave(t *testing.T) {
	m := pairingModel(t, "NEW-9", "new-9-perf", "new-9-main")

	// Accelerator 1 assigns the first configured target (r2perf) to candidate 0.
	next, _ := m.dispatch(ActionPickTarget(1))
	m = next.(Model)
	if c := m.add.candidates[0]; !c.included || c.targetKey != "r2perf" {
		t.Fatalf("accelerator assign: %+v", c)
	}

	// Leave candidate 1 untouched (excluded), then save.
	next, cmd := m.dispatch(ActionConfirm)
	m = next.(Model)
	if m.screen != screenDashboard {
		t.Fatalf("after save: screen = %v, want dashboard", m.screen)
	}
	if cmd == nil {
		t.Fatal("after save: expected a persist+sweep command")
	}

	tk, ok := m.store.Ticket("NEW-9")
	if !ok {
		t.Fatal("NEW-9 not added to store")
	}
	if len(tk.Branches) != 1 || tk.Branches[0].Branch != "new-9-perf" || tk.Branches[0].TargetKey != "r2perf" {
		t.Errorf("saved branches wrong: %+v", tk.Branches)
	}
	if m.cursor != len(m.store.Tickets)-1 {
		t.Errorf("new ticket not selected: cursor = %d", m.cursor)
	}
}

func TestSaveBlocksIncludedButUnassigned(t *testing.T) {
	m := pairingModel(t, "NEW-9", "new-9-a")

	// Include the candidate without giving it a target.
	next, _ := m.dispatch(ActionToggleCandidate)
	m = next.(Model)
	if !m.add.candidates[0].included {
		t.Fatal("toggle did not include the candidate")
	}

	next, _ = m.dispatch(ActionConfirm)
	m = next.(Model)
	if m.screen != screenPairing {
		t.Errorf("blocked save should stay on pairing, got %v", m.screen)
	}
	if !strings.Contains(m.notice, "assign a target") {
		t.Errorf("expected assign-a-target notice, got %q", m.notice)
	}
	if _, ok := m.store.Ticket("NEW-9"); ok {
		t.Error("ticket saved despite an unassigned branch")
	}
}

func TestSaveBareTicketAllowed(t *testing.T) {
	m := pairingModel(t, "NEW-9") // no candidate branches at all
	next, _ := m.dispatch(ActionConfirm)
	m = next.(Model)
	tk, ok := m.store.Ticket("NEW-9")
	if !ok || len(tk.Branches) != 0 {
		t.Errorf("bare ticket not saved cleanly: ok=%v ticket=%+v", ok, tk)
	}
}

func TestPickerAssignsChosenTarget(t *testing.T) {
	m := pairingModel(t, "NEW-9", "new-9-a")

	next, _ := m.dispatch(ActionOpenPicker)
	m = next.(Model)
	if !m.add.picker {
		t.Fatal("picker did not open")
	}
	// Move to the second target (main) and select it.
	next, _ = m.dispatch(ActionMoveDown)
	m = next.(Model)
	next, _ = m.dispatch(ActionConfirm)
	m = next.(Model)

	if m.add.picker {
		t.Error("picker should close on select")
	}
	if c := m.add.candidates[0]; !c.included || c.targetKey != "main" {
		t.Errorf("picker assign: %+v", c)
	}
}

func TestPickTargetOutOfRangeIsNoOp(t *testing.T) {
	m := pairingModel(t, "NEW-9", "new-9-a")
	// Config has two targets; slot 5 is empty.
	next, _ := m.dispatch(ActionPickTarget(5))
	m = next.(Model)
	if c := m.add.candidates[0]; c.included || c.targetKey != "" {
		t.Errorf("out-of-range accelerator assigned a target: %+v", c)
	}
	if m.notice == "" {
		t.Error("expected a notice for the empty slot")
	}
}

func TestAddCancelReturnsHome(t *testing.T) {
	m := pairingModel(t, "NEW-9", "new-9-a")
	next, _ := m.dispatch(ActionCancel)
	m = next.(Model)
	if m.screen != screenDashboard {
		t.Errorf("cancel: screen = %v, want dashboard", m.screen)
	}
	if _, ok := m.store.Ticket("NEW-9"); ok {
		t.Error("cancel should not persist a ticket")
	}
}

// --- delete flow ----------------------------------------------------------

func TestDeleteConfirmRemovesTicket(t *testing.T) {
	m := newModel() // cursor on ABC-1

	next, _ := m.dispatch(ActionDelete)
	m = next.(Model)
	if m.screen != screenConfirmDelete || m.pendingDelete != "ABC-1" {
		t.Fatalf("begin delete: screen=%v pending=%q", m.screen, m.pendingDelete)
	}

	next, cmd := m.dispatch(ActionConfirm)
	m = next.(Model)
	if cmd == nil {
		t.Error("confirmed delete should persist")
	}
	if _, ok := m.store.Ticket("ABC-1"); ok {
		t.Error("ABC-1 still present after confirmed delete")
	}
	if m.screen != screenDashboard {
		t.Errorf("after delete: screen = %v, want dashboard", m.screen)
	}
}

func TestDeleteCancelKeepsTicket(t *testing.T) {
	m := newModel()
	next, _ := m.dispatch(ActionDelete)
	m = next.(Model)
	next, cmd := m.dispatch(ActionCancel)
	m = next.(Model)
	if cmd != nil {
		t.Error("cancelled delete should not persist")
	}
	if _, ok := m.store.Ticket("ABC-1"); !ok {
		t.Error("ABC-1 removed despite cancel")
	}
	if m.screen != screenDashboard || m.pendingDelete != "" {
		t.Errorf("cancel state: screen=%v pending=%q", m.screen, m.pendingDelete)
	}
}

// --- add/delete views -----------------------------------------------------

func TestPairingViewShowsChecklistAndAssignment(t *testing.T) {
	m := pairingModel(t, "NEW-9", "new-9-perf", "new-9-main")
	// Include+assign the first, include-only the second.
	next, _ := m.dispatch(ActionPickTarget(1))
	m = next.(Model)
	next, _ = m.dispatch(ActionMoveDown)
	m = next.(Model)
	next, _ = m.dispatch(ActionToggleCandidate)
	m = next.(Model)

	out := m.View()
	for _, want := range []string{"new-9-perf", "new-9-main", "[x]", "→ r2perf", "pick a target"} {
		if !strings.Contains(out, want) {
			t.Errorf("pairing view missing %q:\n%s", want, out)
		}
	}
}

func TestPickerViewListsTargets(t *testing.T) {
	m := pairingModel(t, "NEW-9", "new-9-a")
	next, _ := m.dispatch(ActionOpenPicker)
	out := next.(Model).View()
	for _, want := range []string{"Target for new-9-a", "r2perf", "origin/main"} {
		if !strings.Contains(out, want) {
			t.Errorf("picker view missing %q:\n%s", want, out)
		}
	}
}

func TestConfirmDeleteViewShowsPrompt(t *testing.T) {
	m := newModel()
	next, _ := m.dispatch(ActionDelete)
	out := next.(Model).View()
	if !strings.Contains(out, "delete ABC-1 and its 2 pairings?") {
		t.Errorf("confirm view missing prompt:\n%s", out)
	}
}

func TestAddIDViewShowsInput(t *testing.T) {
	m := newModel()
	next, _ := m.dispatch(ActionAdd)
	if out := next.(Model).View(); !strings.Contains(out, "New ticket") {
		t.Errorf("add-ID view missing header:\n%s", out)
	}
}

// --- status folding -------------------------------------------------------

func TestApplyStatus(t *testing.T) {
	m := newModel()
	m.loading = true
	msg := statusMsg{
		current: "abc-1-main",
		dirty:   true,
		byKey: map[string]branchStatus{
			statusKey("ABC-1", "abc-1-main"): {ahead: 2, behind: 1, known: true},
		},
	}
	got := m.applyStatus(msg)
	if got.loading {
		t.Error("loading should clear once a sweep lands")
	}
	if got.current != "abc-1-main" || !got.dirty {
		t.Errorf("current/dirty not folded: %q %v", got.current, got.dirty)
	}
	if st := got.status[statusKey("ABC-1", "abc-1-main")]; st.ahead != 2 || st.behind != 1 {
		t.Errorf("status not folded: %+v", st)
	}
}

func TestApplyStatusFetchErrorSurfaces(t *testing.T) {
	got := newModel().applyStatus(statusMsg{fetchErr: context.DeadlineExceeded})
	if !strings.Contains(got.notice, "fetch failed") {
		t.Errorf("fetch error not surfaced in notice: %q", got.notice)
	}
}

func TestApplyStatusDropsStaleSweep(t *testing.T) {
	m := newModel()
	m.sweepID = 5 // a newer sweep is the one we're waiting on
	got := m.applyStatus(statusMsg{id: 3, current: "old", err: context.Canceled})
	if got.current == "old" {
		t.Error("a superseded sweep was folded in")
	}
	if got.err != nil {
		t.Error("a superseded sweep's error surfaced")
	}
}

// --- cancellable fetch ----------------------------------------------------

func TestFetchIsCancellable(t *testing.T) {
	m := newModel()

	next, cmd := m.startSweep(true)
	m = next.(Model)
	if cmd == nil {
		t.Fatal("fetch returned no command")
	}
	if m.fetchCancel == nil || !m.loading {
		t.Fatalf("fetch not marked in-flight: cancel=%v loading=%v", m.fetchCancel != nil, m.loading)
	}
	if !strings.Contains(m.notice, "esc to cancel") {
		t.Errorf("fetch notice should advertise cancel: %q", m.notice)
	}
	fetchID := m.sweepID

	// esc cancels: spinner stops, fetch is released, a notice explains.
	next, _ = m.dispatch(ActionCancel)
	m = next.(Model)
	if m.fetchCancel != nil {
		t.Error("cancel left the fetch in-flight")
	}
	if m.loading {
		t.Error("cancel did not stop the spinner")
	}
	if !strings.Contains(m.notice, "cancel") {
		t.Errorf("cancel notice = %q", m.notice)
	}

	// The cancelled fetch's sweep, landing late, must not clobber state.
	got := m.applyStatus(statusMsg{id: fetchID, current: "stale", err: context.Canceled})
	if got.current == "stale" || got.err != nil {
		t.Errorf("cancelled sweep folded in: current=%q err=%v", got.current, got.err)
	}
}

func TestEscIsNoOpWithoutFetch(t *testing.T) {
	// esc on an idle dashboard has nothing to cancel.
	next, cmd := newModel().dispatch(ActionCancel)
	m := next.(Model)
	if cmd != nil {
		t.Error("idle esc issued a command")
	}
	if m.notice != "" || m.loading {
		t.Errorf("idle esc changed state: notice=%q loading=%v", m.notice, m.loading)
	}
}

func TestRefreshIsNotCancellable(t *testing.T) {
	// A plain refresh is local and fast; esc must not abort it.
	next, _ := newModel().startSweep(false)
	m := next.(Model)
	if m.fetchCancel != nil {
		t.Fatal("plain refresh should not be cancellable")
	}
	next, _ = m.dispatch(ActionCancel)
	if !next.(Model).loading {
		t.Error("esc during a refresh aborted it")
	}
}

// --- view -----------------------------------------------------------------

func TestSelectBandSpansWidestRow(t *testing.T) {
	m := newModel()
	rows := []string{"short", "a decidedly longer row", "mid"}

	out := selectBand(m.styles, m.width, rows, 0)
	if got, want := lipgloss.Width(out[0]), lipgloss.Width(rows[1]); got != want {
		t.Errorf("band width = %d, want the widest row's %d", got, want)
	}
	if out[2] != rows[2] {
		t.Error("selectBand disturbed a non-selected row")
	}

	// No active selection leaves every row untouched.
	none := selectBand(m.styles, m.width, rows, -1)
	for i := range rows {
		if none[i] != rows[i] {
			t.Errorf("selectBand(-1) changed row %d", i)
		}
	}
}

func TestSelectBandFillsPanelWidth(t *testing.T) {
	m := newModel()
	m.width = 80 // once the size is known, the band fills the whole panel
	rows := []string{"short", "a mid row"}
	if contentWidth(m.styles, m.width) <= lipgloss.Width(rows[1]) {
		t.Fatal("precondition: an 80-col panel should exceed this content")
	}
	out := selectBand(m.styles, m.width, rows, 0)
	if got, want := lipgloss.Width(out[0]), contentWidth(m.styles, m.width); got != want {
		t.Errorf("band width = %d, want the panel's inner width %d", got, want)
	}
}

func TestViewEmptyStateTeaches(t *testing.T) {
	m := New(git.New(t_nowhere), samplePaths(), sampleConfig(), store.Store{}, store.Prefs{})
	m.loading = false
	out := m.View()
	if !strings.Contains(out, "No tickets tracked") {
		t.Errorf("empty state missing teach line:\n%s", out)
	}
}

func TestViewRendersStatusCluster(t *testing.T) {
	m := newModel()
	m.expanded["ABC-1"] = true
	m.current = "abc-1-main"
	m.dirty = true
	m.status = map[string]branchStatus{
		statusKey("ABC-1", "abc-1-perf"): {behind: 3, ahead: 1, known: true},
		statusKey("ABC-1", "abc-1-main"): {behind: 0, ahead: 0, known: true},
	}
	out := m.View()

	for _, want := range []string{"abc-1-perf", "abc-1-main", "↓3", "↑1", "r2perf"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
}

// The publish signal (roadmap 17b). Without it `u`'s push is invisible: today's
// ↓behind ↑ahead measures against the *target*, so a branch merged locally and
// one merged and published render identically, and the difference between the
// two verbs would live only in the help.
func TestViewRendersThePublishSignal(t *testing.T) {
	tests := []struct {
		name   string
		st     branchStatus
		want   string
		absent []string
	}{
		{"unpublished commits", branchStatus{known: true, unpublished: 2}, "⇡", []string{"⊘"}},
		{"no upstream at all", branchStatus{known: true, noUpstream: true}, "⊘", []string{"⇡"}},
		{"published and current", branchStatus{known: true}, "", []string{"⇡", "⊘"}},
		// A degraded probe makes no claim rather than a wrong one — the same rule
		// the unmergeable marker follows.
		{"probe degraded", branchStatus{known: true, ahead: 4}, "", []string{"⇡", "⊘"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel()
			m.expanded["ABC-2"] = true
			m.status = map[string]branchStatus{statusKey("ABC-2", "abc-2"): tc.st}
			out := m.View()

			if tc.want != "" && !strings.Contains(out, tc.want) {
				t.Errorf("view missing %q:\n%s", tc.want, out)
			}
			for _, no := range tc.absent {
				if strings.Contains(out, no) {
					t.Errorf("view claims %q, which is not this branch's state:\n%s", no, out)
				}
			}
		})
	}
}

// The glyph is two cells whatever state it is in, so the two beside it stay in
// one column down the list — "aligned so the eye scans down" (DESIGN.md §1) is
// what the cluster is for, and a variable-width cell breaks it for every row.
func TestPublishGlyphIsOneFixedWidthColumn(t *testing.T) {
	m := newModel()
	states := []branchStatus{
		{known: true, unpublished: 3},
		{known: true, noUpstream: true},
		{known: true},
	}
	want := lipgloss.Width(m.renderPublish(states[0]))
	for _, st := range states[1:] {
		if got := lipgloss.Width(m.renderPublish(st)); got != want {
			t.Errorf("renderPublish(%+v) is %d cells, want %d — the column has to hold", st, got, want)
		}
	}
}

// The two signals are about different remotes, and the row is the only place
// that has to be unambiguous about it: ↑N counts against the target, ⇡ against
// origin/<branch>. A branch can be level with its target and still unpublished,
// which is exactly the state `s` leaves behind.
func TestPublishSignalIsIndependentOfTheTarget(t *testing.T) {
	m := newModel()
	m.expanded["ABC-2"] = true
	m.status = map[string]branchStatus{
		statusKey("ABC-2", "abc-2"): {known: true, ahead: 0, behind: 0, unpublished: 2},
	}
	out := m.View()

	if !strings.Contains(out, "↓0 ↑0") {
		t.Errorf("view lost the target comparison:\n%s", out)
	}
	if !strings.Contains(out, "⇡") {
		t.Errorf("a branch level with its target but unpublished shows nothing:\n%s", out)
	}
}

func TestViewRendersUnknownTarget(t *testing.T) {
	st := store.Store{Tickets: []store.Ticket{
		{ID: "X", Branches: []store.TicketBranch{{Branch: "b", TargetKey: "gone"}}},
	}}
	m := New(git.New(t_nowhere), samplePaths(), sampleConfig(), st, store.Prefs{})
	m.loading = false
	m.expanded["X"] = true
	m.status = map[string]branchStatus{statusKey("X", "b"): {known: false}}
	if out := m.View(); !strings.Contains(out, "unknown target") {
		t.Errorf("view did not flag a stale pairing:\n%s", out)
	}
}

// --- sweep (real repo) ----------------------------------------------------

func TestSweepComputesAheadBehindAndRoutesUnknownTarget(t *testing.T) {
	dir := newTestRepo(t)
	// feature branches off main, gains a commit (ahead=1); main then moves on
	// (behind=1).
	rungit(t, dir, "branch", "feature")
	rungit(t, dir, "branch", "solo") // paired to a target that isn't in config
	rungit(t, dir, "checkout", "--quiet", "feature")
	writeCommit(t, dir, "f.txt", "feature work")
	rungit(t, dir, "checkout", "--quiet", "main")
	writeCommit(t, dir, "m.txt", "main moves")

	repo := git.New(dir)
	cfg := store.Config{Targets: []store.Target{{Key: "main", Ref: "main"}}}
	tickets := []store.Ticket{{ID: "T", Branches: []store.TicketBranch{
		{Branch: "feature", TargetKey: "main"}, // resolvable
		{Branch: "solo", TargetKey: "ghost"},   // target absent from config
	}}}

	msg := sweep(context.Background(), repo, cfg, tickets, false)
	if msg.err != nil {
		t.Fatalf("sweep: %v", msg.err)
	}
	if msg.current != "main" {
		t.Errorf("current = %q, want main", msg.current)
	}

	known := msg.byKey[statusKey("T", "feature")]
	if !known.known || known.err != nil {
		t.Fatalf("resolvable branch: %+v", known)
	}
	if known.ahead != 1 || known.behind != 1 {
		t.Errorf("ahead/behind = %d/%d, want 1/1", known.ahead, known.behind)
	}

	if ghost := msg.byKey[statusKey("T", "solo")]; ghost.known {
		t.Errorf("branch with absent target key should route to known=false: %+v", ghost)
	}
}

// The publish half of the sweep, against a real remote (roadmap 17b). The three
// answers are deliberately distinct: a branch that has never been published is
// not a branch with zero unpublished commits, and neither is a branch whose
// probe could not run.
func TestSweepReadsEachBranchAgainstItsOwnRemote(t *testing.T) {
	origin := updateOrigin(t) // main and feature, both published
	dir := updateClone(t, origin)

	// feature gains a commit that never leaves this machine — the state `s` is
	// designed to leave behind, and the one the dashboard could not see.
	rungit(t, dir, "checkout", "--quiet", "feature")
	writeCommit(t, dir, "mine.txt", "mine\n")
	rungit(t, dir, "checkout", "--quiet", "main")
	rungit(t, dir, "branch", "never-pushed")

	cfg := store.Config{Targets: []store.Target{{Key: "main", Ref: "origin/main"}}}
	tickets := []store.Ticket{{ID: "T", Branches: []store.TicketBranch{
		{Branch: "feature", TargetKey: "main"},
		{Branch: "never-pushed", TargetKey: "main"},
		{Branch: "main", TargetKey: "main"},
	}}}

	msg := sweep(context.Background(), git.New(dir), cfg, tickets, false)
	if msg.err != nil {
		t.Fatalf("sweep: %v", msg.err)
	}

	if got := msg.byKey[statusKey("T", "feature")]; got.unpublished != 1 || got.noUpstream {
		t.Errorf("feature = %+v, want 1 unpublished commit and an upstream", got)
	}
	if got := msg.byKey[statusKey("T", "never-pushed")]; !got.noUpstream || got.unpublished != 0 {
		t.Errorf("never-pushed = %+v, want no upstream — that is a third answer, not zero", got)
	}
	if got := msg.byKey[statusKey("T", "main")]; got.unpublished != 0 || got.noUpstream {
		t.Errorf("main = %+v, want published and current", got)
	}
}

// --- test-repo helpers ----------------------------------------------------

func rungit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// gitOut is rungit for the cases that need the answer rather than the effect —
// reading a ref straight out of a bare origin, where the git wrapper's own calls
// would be the thing under test rather than the instrument.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	rungit(t, dir, "init", "--quiet", "--initial-branch=main")
	rungit(t, dir, "config", "user.email", "test@example.com")
	rungit(t, dir, "config", "user.name", "Test")
	writeCommit(t, dir, "seed.txt", "seed")
	return dir
}

func writeCommit(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	rungit(t, dir, "add", name)
	rungit(t, dir, "commit", "--quiet", "-m", "add "+name)
}

// --- area 5: unmergeable detection + diff panel ---------------------------

func TestSweepDetectsUnmergeableCollision(t *testing.T) {
	dir := newTestRepo(t)
	// Seed the files and the declaration, then branch so both sides carry them.
	writeCommit(t, dir, ".gitattributes", "*.uwe -merge\n")
	writeCommit(t, dir, "flow.uwe", "v0")
	writeCommit(t, dir, "code.go", "package a\n")
	writeCommit(t, dir, "gone.uwe", "v0")
	rungit(t, dir, "branch", "feature")
	rungit(t, dir, "branch", "insync") // never diverges: behind 0, so never scanned

	// feature changes an unmergeable file and a mergeable one.
	rungit(t, dir, "checkout", "--quiet", "feature")
	writeCommit(t, dir, "flow.uwe", "v-feature")
	writeCommit(t, dir, "code.go", "package a // feature\n")

	// main moves under it: same two files, plus one only it touched.
	rungit(t, dir, "checkout", "--quiet", "main")
	writeCommit(t, dir, "flow.uwe", "v-main")
	writeCommit(t, dir, "code.go", "package a // main\n")
	writeCommit(t, dir, "gone.uwe", "v-main")

	repo := git.New(dir)
	cfg := store.Config{Targets: []store.Target{{Key: "main", Ref: "main"}}}
	tickets := []store.Ticket{{ID: "T", Branches: []store.TicketBranch{
		{Branch: "feature", TargetKey: "main"},
		{Branch: "insync", TargetKey: "main"},
	}}}

	msg := sweep(context.Background(), repo, cfg, tickets, false)
	if msg.err != nil {
		t.Fatalf("sweep: %v", msg.err)
	}

	feat := msg.byKey[statusKey("T", "feature")]
	// Only flow.uwe survives: code.go collides but is mergeable; gone.uwe is
	// unmergeable but only the target changed it, so it is not a collision.
	if got := strings.Join(paths(feat.unmergeable), ","); got != "flow.uwe" {
		t.Errorf("feature unmergeable = %q, want %q", got, "flow.uwe")
	}
	// It is flagged by git's own attribute, not only by a config glob — the state
	// the declare flow exists to bring about, and what the panel badges per file.
	if !feat.unmergeable[0].declared {
		t.Error("flow.uwe is -merge in .gitattributes but was not recorded as declared to git")
	}

	// The in-sync branch never moved behind the target, so detection is skipped
	// entirely — no collision, and no wasted diff work.
	if in := msg.byKey[statusKey("T", "insync")]; len(in.unmergeable) != 0 {
		t.Errorf("in-sync branch flagged unmergeable: %v", in.unmergeable)
	}
}

func TestDetectUnmergeableCountsWorkingTreeEdits(t *testing.T) {
	dir := newTestRepo(t)
	writeCommit(t, dir, ".gitattributes", "*.uwe -merge\n")
	writeCommit(t, dir, "flow.uwe", "v0")
	rungit(t, dir, "branch", "feature")

	// The target moves the unmergeable file; the branch itself has NO committed
	// change to it.
	rungit(t, dir, "checkout", "--quiet", "main")
	writeCommit(t, dir, "flow.uwe", "v-main")

	// The branch's only edit is uncommitted, in the working tree.
	rungit(t, dir, "checkout", "--quiet", "feature")
	if err := os.WriteFile(filepath.Join(dir, "flow.uwe"), []byte("v-wip"), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := git.New(dir)
	cfg := store.Config{}
	ctx := context.Background()

	// Committed-only: nothing collides, so nothing is flagged.
	if got, err := detectUnmergeable(ctx, repo, cfg, "feature", "main", nil); err != nil {
		t.Fatal(err)
	} else if len(got) != 0 {
		t.Errorf("committed-only detection = %v, want empty", got)
	}

	// With the working-tree edit unioned in, the collision appears — the decision
	// that uncommitted local edits count.
	wt, err := repo.WorkingTreeModified(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, err := detectUnmergeable(ctx, repo, cfg, "feature", "main", toSet(wt))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(paths(got), ",") != "flow.uwe" {
		t.Errorf("working-tree detection = %v, want [flow.uwe]", got)
	}
}

func TestDetectUnmergeableConfigGlob(t *testing.T) {
	// No .gitattributes at all — the config-glob half of the hybrid rule must
	// carry detection on its own.
	dir := newTestRepo(t)
	writeCommit(t, dir, "flow.uwe", "v0")
	rungit(t, dir, "branch", "feature")
	rungit(t, dir, "checkout", "--quiet", "feature")
	writeCommit(t, dir, "flow.uwe", "v-feature")
	rungit(t, dir, "checkout", "--quiet", "main")
	writeCommit(t, dir, "flow.uwe", "v-main")

	cfg := store.Config{Unmergeable: []store.Unmergeable{{Name: "wf", Globs: []string{"*.uwe"}}}}
	got, err := detectUnmergeable(context.Background(), git.New(dir), cfg, "feature", "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(paths(got), ",") != "flow.uwe" {
		t.Errorf("config-glob detection = %v, want [flow.uwe]", got)
	}
	// Only Drift knows: git has no .gitattributes here, so the panel must show it
	// as undeclared and `w` must have something real to do.
	if got[0].declared {
		t.Error("a config-glob match was reported as declared to git")
	}
}

// branchDiffModel returns a dashboard with ABC-1 expanded, the cursor on its
// first branch, and that branch carrying `files` as unmergeable collisions.
func branchDiffModel(files ...string) Model {
	m := newModel()
	m.expanded["ABC-1"] = true
	m.cursor = 1 // ABC-1 headline is row 0; its first branch is row 1
	cs := make([]collision, len(files))
	for i, f := range files {
		cs[i] = collision{path: f} // undeclared: only Drift's config globs know
	}
	m.status[statusKey("ABC-1", "abc-1-perf")] = branchStatus{known: true, behind: 2, unmergeable: cs}
	return m
}

func TestBranchRowIsSelectable(t *testing.T) {
	m := branchDiffModel()
	row, ok := m.selectedRow()
	if !ok || !row.isBranch() {
		t.Fatalf("cursor on an expanded ticket's first branch: row=%+v ok=%v", row, ok)
	}
	if br := m.store.Tickets[row.ticket].Branches[row.branch].Branch; br != "abc-1-perf" {
		t.Errorf("selected branch = %q, want abc-1-perf", br)
	}
}

func TestEnterOnBranchOpensDiff(t *testing.T) {
	m := branchDiffModel("flow.uwe", "scene.unity")
	next, cmd := m.dispatch(ActionToggleExpand)
	mm := next.(Model)
	if mm.screen != screenDiff {
		t.Fatalf("screen = %v, want screenDiff", mm.screen)
	}
	if strings.Join(paths(mm.diff.files), ",") != "flow.uwe,scene.unity" {
		t.Errorf("diff.files = %v", mm.diff.files)
	}
	if mm.diff.branch != "abc-1-perf" || mm.diff.targetKey != "r2perf" {
		t.Errorf("diff session = %q -> %q, want abc-1-perf -> r2perf", mm.diff.branch, mm.diff.targetKey)
	}
	if cmd == nil {
		t.Error("opening a diff should fetch the first file")
	}
}

func TestEnterOnBranchWithNoCollisionStaysHome(t *testing.T) {
	m := branchDiffModel() // no unmergeable files
	next, _ := m.dispatch(ActionToggleExpand)
	mm := next.(Model)
	if mm.screen != screenDashboard {
		t.Errorf("screen = %v, want to stay on the dashboard", mm.screen)
	}
	if !strings.Contains(mm.notice, "no unmergeable") {
		t.Errorf("notice = %q, want it to explain there is nothing to reconcile", mm.notice)
	}
}

func TestApplyDiffPopulatesAndDiscardsStale(t *testing.T) {
	m := branchDiffModel("flow.uwe")
	next, _ := m.dispatch(ActionToggleExpand)
	m = next.(Model)

	// A diff for the wrong branch is discarded, never painted into the panel.
	m = m.applyDiff(diffMsg{branch: "other", targetRef: m.diff.targetRef, path: "flow.uwe", content: "STALE"})
	if _, cached := m.diff.cache["flow.uwe"]; cached {
		t.Error("a diff from another branch was cached")
	}

	// The matching diff lands and is cached.
	m = m.applyDiff(diffMsg{branch: "abc-1-perf", targetRef: m.diff.targetRef, path: "flow.uwe", content: "@@ real diff @@"})
	if entry, ok := m.diff.cache["flow.uwe"]; !ok || entry.content != "@@ real diff @@" {
		t.Errorf("matching diff not cached: %+v", m.diff.cache)
	}
	if !strings.Contains(m.diff.vp.View(), "real diff") {
		t.Errorf("viewport did not show the loaded diff:\n%s", m.diff.vp.View())
	}
}

// Reconciling a branch's collisions is a round trip, so the file list wraps in
// both directions rather than dead-ending at either end.
func TestDiffFileCyclingWraps(t *testing.T) {
	m := branchDiffModel("a.uwe", "b.uwe", "c.uwe")
	next, _ := m.dispatch(ActionToggleExpand)
	m = next.(Model)

	// shift+tab at the first file wraps to the last.
	back, _ := m.dispatchDiff(ActionPrevFile)
	if got := back.(Model).diff.cursor; got != 2 {
		t.Errorf("prev at the first file: cursor = %d, want the last (2)", got)
	}

	// tab walks forward and comes back around to the first.
	for want := 1; want <= 2; want++ {
		fwd, _ := m.dispatchDiff(ActionNextFile)
		m = fwd.(Model)
		if m.diff.cursor != want {
			t.Fatalf("next: cursor = %d, want %d", m.diff.cursor, want)
		}
	}
	wrapped, _ := m.dispatchDiff(ActionNextFile)
	if got := wrapped.(Model).diff.cursor; got != 0 {
		t.Errorf("next past the last file: cursor = %d, want to wrap to 0", got)
	}
}

func TestDiffFileCyclingWithOneFile(t *testing.T) {
	m := branchDiffModel("only.uwe")
	next, _ := m.dispatch(ActionToggleExpand)
	m = next.(Model)

	for _, action := range []Action{ActionNextFile, ActionPrevFile} {
		got, _ := m.dispatchDiff(action)
		if c := got.(Model).diff.cursor; c != 0 {
			t.Errorf("%s with a single file: cursor = %d, want 0", action, c)
		}
	}
}

func TestDiffEscReturnsHome(t *testing.T) {
	m := branchDiffModel("flow.uwe")
	next, _ := m.dispatch(ActionToggleExpand)
	m = next.(Model)

	home, _ := m.dispatchDiff(ActionCancel)
	mm := home.(Model)
	if mm.screen != screenDashboard {
		t.Errorf("esc from diff: screen = %v, want dashboard", mm.screen)
	}
	if len(mm.diff.files) != 0 {
		t.Errorf("esc from diff left session state behind: %+v", mm.diff)
	}
}

func TestBranchRowShowsUnmergeableMarker(t *testing.T) {
	m := branchDiffModel("flow.uwe", "scene.unity")
	m.width = 120
	out := m.dashboardView()
	if !strings.Contains(out, "2 unmergeable") {
		t.Errorf("dashboard did not flag the unmergeable branch:\n%s", out)
	}
}

// --- declaring a file unmergeable (area 5, part 2) --------------------------

// declareModel opens the diff panel on a file, ready for w. The config carries
// one unmergeable class so the pattern step has both kinds of choice to offer.
func declareModel(t *testing.T) Model {
	t.Helper()
	m := branchDiffModel("workflows/onboarding/flow.uwe")
	m.cfg.Unmergeable = []store.Unmergeable{
		{Name: "workflows", Globs: []string{"workflows/**/*.uwe"}},
	}
	next, _ := m.dispatch(ActionToggleExpand)
	return next.(Model)
}

func TestDeclareOffersClassGlobAndTheFileItself(t *testing.T) {
	m := declareModel(t)

	next, _ := m.dispatchDiff(ActionDeclare)
	m = next.(Model)
	if !m.diff.declare.open || m.diff.declare.step != stepPattern {
		t.Fatalf("w did not open the pattern step: %+v", m.diff.declare)
	}

	got := m.diff.declare.patterns
	if len(got) != 2 {
		t.Fatalf("patterns = %+v, want the config glob and the file's own path", got)
	}
	if got[0].pattern != "workflows/**/*.uwe" || !strings.Contains(got[0].why, "workflows") {
		t.Errorf("first choice = %+v, want the matched class glob", got[0])
	}
	if got[1].pattern != "workflows/onboarding/flow.uwe" {
		t.Errorf("second choice = %+v, want the file itself", got[1])
	}
}

// A file flagged only by check-attr matches no config glob, so the path is the
// one thing left to offer — never an empty list.
func TestDeclareAlwaysOffersThePath(t *testing.T) {
	m := branchDiffModel("scene.unity")
	next, _ := m.dispatch(ActionToggleExpand)
	m = next.(Model)

	next, _ = m.dispatchDiff(ActionDeclare)
	got := next.(Model).diff.declare.patterns
	if len(got) != 1 || got[0].pattern != "scene.unity" {
		t.Errorf("patterns = %+v, want just the file's own path", got)
	}
}

func TestDeclareStepsPatternThenDestinationThenWrites(t *testing.T) {
	m := declareModel(t)
	next, _ := m.dispatchDiff(ActionDeclare)
	m = next.(Model)

	// Choosing the second pattern (the file itself) moves to the destination step.
	next, _ = m.dispatchDeclare(ActionMoveDown)
	next, _ = next.(Model).dispatchDeclare(ActionConfirm)
	m = next.(Model)
	if m.diff.declare.step != stepDest {
		t.Fatalf("step = %v, want the destination question", m.diff.declare.step)
	}
	if m.diff.declare.pattern != "workflows/onboarding/flow.uwe" {
		t.Errorf("chosen pattern = %q", m.diff.declare.pattern)
	}
	if m.diff.declare.cursor != 0 {
		t.Errorf("destination cursor = %d, want a fresh list", m.diff.declare.cursor)
	}

	// Confirming a destination closes the overlay and hands off the write.
	next, cmd := m.dispatchDeclare(ActionConfirm)
	m = next.(Model)
	if m.diff.declare.open {
		t.Error("the overlay stayed open after the write was dispatched")
	}
	if cmd == nil {
		t.Fatal("confirming a destination should write")
	}
	if m.screen != screenDiff {
		t.Errorf("screen = %v, want to stay on the diff panel", m.screen)
	}
}

func TestDeclareEscUnwindsOneStepAtATime(t *testing.T) {
	m := declareModel(t)
	next, _ := m.dispatchDiff(ActionDeclare)
	next, _ = next.(Model).dispatchDeclare(ActionMoveDown)
	next, _ = next.(Model).dispatchDeclare(ActionConfirm) // now on the destination step

	// First esc: back to the pattern list, on the choice just made.
	next, _ = next.(Model).dispatchDeclare(ActionCancel)
	m = next.(Model)
	if m.diff.declare.step != stepPattern {
		t.Fatalf("esc from the destination step: step = %v, want the pattern list", m.diff.declare.step)
	}
	if m.diff.declare.cursor != 1 {
		t.Errorf("cursor = %d, want to land back on the chosen pattern", m.diff.declare.cursor)
	}

	// Second esc: the overlay closes and the diff is still there underneath.
	next, _ = m.dispatchDeclare(ActionCancel)
	m = next.(Model)
	if m.diff.declare.open {
		t.Error("second esc did not close the overlay")
	}
	if m.screen != screenDiff || len(m.diff.files) == 0 {
		t.Errorf("closing the overlay disturbed the diff panel: screen=%v files=%v", m.screen, m.diff.files)
	}
}

// While a choice is open, j/k must move the cursor — not scroll the diff
// underneath, which is what an unbound key does on the panel itself.
func TestDeclareOverlaySwallowsScrollKeys(t *testing.T) {
	m := declareModel(t)
	next, _ := m.dispatchDiff(ActionDeclare)
	m = next.(Model)

	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = next.(Model)
	if m.diff.declare.cursor != 1 {
		t.Errorf("j in the overlay: cursor = %d, want 1", m.diff.declare.cursor)
	}
	// An unbound key does nothing at all rather than reaching the viewport.
	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	if next.(Model).diff.declare.cursor != 1 {
		t.Error("an unbound key disturbed the overlay")
	}
}

func TestApplyDeclareReportsWhatHappened(t *testing.T) {
	m := declareModel(t)

	// A fresh write names the destination it landed in.
	written, _ := m.applyDeclare(declareMsg{
		dest: git.AttrLocal,
		decl: git.AttrDeclaration{Path: "/repo/.git/info/attributes", Pattern: "*.uwe"},
	})
	if !strings.Contains(written.notice, "*.uwe -merge") || !strings.Contains(written.notice, "info/attributes") {
		t.Errorf("notice = %q, want the pattern and where it landed", written.notice)
	}

	// Already declared is a success — git knowing is the whole point.
	already, _ := m.applyDeclare(declareMsg{
		dest: git.AttrRepo,
		decl: git.AttrDeclaration{Path: "/repo/.gitattributes", Pattern: "*.uwe", Already: true},
	})
	if !strings.Contains(already.notice, "already") {
		t.Errorf("notice = %q, want it to say the rule was already there", already.notice)
	}

	failed, _ := m.applyDeclare(declareMsg{dest: git.AttrRepo, err: os.ErrPermission})
	if !strings.Contains(failed.notice, "declare failed") {
		t.Errorf("notice = %q, want the failure surfaced", failed.notice)
	}
}

// A committed .gitattributes is a working-tree change, so the dirty dot is stale
// the moment it is written; a local write touches nothing git tracks. Both
// return a command either way — every successful declare re-reads git's answer
// for the panel — so the sweep is identified by the state startSweep leaves.
func TestApplyDeclareResweepsOnlyAfterASharedWrite(t *testing.T) {
	m := declareModel(t)

	shared, cmd := m.applyDeclare(declareMsg{
		dest: git.AttrRepo,
		decl: git.AttrDeclaration{Path: "/repo/.gitattributes", Pattern: "*.uwe"},
	})
	if !shared.loading || shared.sweepID == m.sweepID {
		t.Error("a shared write should re-sweep so the dirty dot is current")
	}
	if cmd == nil {
		t.Error("expected the sweep and the attribute re-read")
	}
	if !strings.Contains(shared.notice, "declared") {
		t.Errorf("notice = %q — the sweep must not clear the result", shared.notice)
	}

	local, cmd := m.applyDeclare(declareMsg{
		dest: git.AttrLocal,
		decl: git.AttrDeclaration{Path: "/repo/.git/info/attributes", Pattern: "*.uwe"},
	})
	if local.loading || local.sweepID != m.sweepID {
		t.Error("a local write changes nothing git tracks; no sweep needed")
	}
	if cmd == nil {
		t.Error("a local write should still re-read git's answer for the panel")
	}
}

// The badge is the visible half of declaring: it comes from git's own re-read,
// so a glob covering several of the listed files flips all of them at once.
func TestApplyDeclaredFlipsTheBadgeFromGitsAnswer(t *testing.T) {
	m := declareModel(t)
	if m.diff.files[0].declared {
		t.Fatal("fixture should start undeclared")
	}

	got := m.applyDeclared(declaredMsg{
		branch:    m.diff.branch,
		targetRef: m.diff.targetRef,
		byPath:    map[string]bool{m.diff.files[0].path: true},
	})
	if !got.diff.files[0].declared {
		t.Error("git reported the file declared; the badge did not flip")
	}

	// A reply for a branch the user has since left is discarded.
	stale := m.applyDeclared(declaredMsg{branch: "other", targetRef: m.diff.targetRef,
		byPath: map[string]bool{m.diff.files[0].path: true}})
	if stale.diff.files[0].declared {
		t.Error("a re-read from another branch was applied to this panel")
	}

	// So is a failed one — better to keep showing the last known truth.
	failed := m.applyDeclared(declaredMsg{branch: m.diff.branch, targetRef: m.diff.targetRef,
		byPath: map[string]bool{m.diff.files[0].path: true}, err: os.ErrPermission})
	if failed.diff.files[0].declared {
		t.Error("a failed re-read was applied")
	}
}

func TestDiffPanelBadgesWhetherGitKnows(t *testing.T) {
	m := declareModel(t)
	m.width = 120

	out := m.diffView()
	if !strings.Contains(out, "not declared to git") {
		t.Errorf("panel did not say git is unaware of this file:\n%s", out)
	}

	m.diff.files[0].declared = true
	if out := m.diffView(); !strings.Contains(out, "declared to git") ||
		strings.Contains(out, "not declared to git") {
		t.Errorf("panel did not flip to the declared badge:\n%s", out)
	}
}

// The whole point of the config allowlist: a team without a committed
// .gitattributes must never see it offered, so it cannot be picked by accident.
func TestDeclareDestinationsHonorTheConfigAllowlist(t *testing.T) {
	both := declareDests(store.Config{})
	if len(both) != 2 {
		t.Errorf("unconstrained config offered %d destinations, want both", len(both))
	}

	localOnly := declareDests(store.Config{Declare: &store.Declare{
		Destinations: []string{store.DestLocal},
	}})
	if len(localOnly) != 1 || localOnly[0] != git.AttrLocal {
		t.Fatalf("allowlist ignored: %v", localOnly)
	}

	// And it reorders, not just filters.
	reordered := declareDests(store.Config{Declare: &store.Declare{
		Destinations: []string{store.DestLocal, store.DestShared},
	}})
	if len(reordered) != 2 || reordered[0] != git.AttrLocal {
		t.Errorf("config order not honored: %v", reordered)
	}
}

func TestDeclareOverlayShowsOnlyAllowedDestinations(t *testing.T) {
	m := declareModel(t)
	m.width = 120
	m.cfg.Declare = &store.Declare{Destinations: []string{store.DestLocal}}

	next, _ := m.dispatchDiff(ActionDeclare)
	next, _ = next.(Model).dispatchDeclare(ActionConfirm) // past the pattern step
	m = next.(Model)

	out := m.diffView()
	if strings.Contains(out, ".gitattributes") {
		t.Errorf("a destination excluded by config was still offered:\n%s", out)
	}
	if !strings.Contains(out, "info/attributes") {
		t.Errorf("the allowed destination is missing:\n%s", out)
	}

	// And the excluded one cannot be reached by moving the cursor.
	moved, _ := m.dispatchDeclare(ActionMoveDown)
	if c := moved.(Model).diff.declare.cursor; c != 0 {
		t.Errorf("cursor moved to %d with a single destination", c)
	}
}

func TestDeclareViewShowsBothQuestions(t *testing.T) {
	m := declareModel(t)
	m.width = 120
	next, _ := m.dispatchDiff(ActionDeclare)
	m = next.(Model)

	out := m.diffView()
	if !strings.Contains(out, "workflows/**/*.uwe") || !strings.Contains(out, "this file only") {
		t.Errorf("pattern step did not show both choices with their reason:\n%s", out)
	}

	next, _ = m.dispatchDeclare(ActionConfirm)
	out = next.(Model).diffView()
	if !strings.Contains(out, ".gitattributes") || !strings.Contains(out, "info/attributes") {
		t.Errorf("destination step did not offer both destinations:\n%s", out)
	}
	if !strings.Contains(out, "committed") || !strings.Contains(out, "local only") {
		t.Errorf("destination step did not name each consequence:\n%s", out)
	}
}

// --- panel geometry ---------------------------------------------------------

// The bug this pins: lipgloss counts a style's horizontal padding inside
// Width() but not its border, so a panel set to contentWidth has a text area
// two cells narrower than the rows built to fill it — and every selected row
// wrapped, dropping its tail (the "⚠ N unmergeable" marker, most visibly) onto
// the next line. It hid in tests because the ASCII color profile lets lipgloss
// trim the band's trailing spaces; asserting on the geometry catches it whatever
// the profile.
func TestPanelTextAreaMatchesTheBandWidth(t *testing.T) {
	s := newStyles(store.Prefs{})
	for _, width := range []int{40, 80, 100, 137, 200} {
		panel := panelStyle(s, width)
		textArea := panel.GetWidth() - panel.GetHorizontalPadding()

		if cw := contentWidth(s, width); textArea != cw {
			t.Errorf("width %d: panel text area = %d, contentWidth = %d — rows built to fill it will wrap",
				width, textArea, cw)
		}

		// And the panel still spans the terminal: no dead columns on the right.
		total := panel.GetWidth() + panel.GetHorizontalBorderSize() + s.app.GetHorizontalFrameSize()
		if total != width {
			t.Errorf("width %d: panel spans %d columns, want the full width", width, total)
		}
	}
}

func TestSelectedRowFitsThePanelWithoutWrapping(t *testing.T) {
	const width = 100
	s := newStyles(store.Prefs{})
	rows := selectBand(s, width, []string{"a branch row", "another"}, 0)

	panel := panelStyle(s, width)
	textArea := panel.GetWidth() - panel.GetHorizontalPadding()
	if got := lipgloss.Width(rows[0]); got != textArea {
		t.Errorf("selection band = %d wide, panel text area = %d — a band wider than the panel wraps", got, textArea)
	}
}

func TestPanelFallsBackBeforeTheFirstWindowSize(t *testing.T) {
	s := newStyles(store.Prefs{})
	if w := panelStyle(s, 0).GetWidth(); w != 0 {
		t.Errorf("unknown terminal size: panel width = %d, want natural content sizing", w)
	}
}

// --- diff coloring ----------------------------------------------------------

func TestDiffLineRoles(t *testing.T) {
	tests := []struct {
		line string
		want diffRole
	}{
		{"+++ b/workflows/flow.uwe", diffMeta}, // header, not an added line
		{"--- a/workflows/flow.uwe", diffMeta}, // header, not a removed line
		{"@@ -1,4 +1,5 @@", diffHunk},
		{`+  <step id="3"/>`, diffAdded},
		{`-  <step id="2"/>`, diffRemoved},
		{`   <step id="1"/>`, diffContext},
		{"diff --git a/x b/x", diffMeta},
		{"index 1a2b3c4..5d6e7f8 100644", diffMeta},
		{`\ No newline at end of file`, diffMeta},
		{"", diffContext},
	}
	for _, tt := range tests {
		if got := diffLineRole(tt.line); got != tt.want {
			t.Errorf("diffLineRole(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestColorizeDiffKeepsEveryLine(t *testing.T) {
	raw := "--- a/x\n+++ b/x\n@@ -1,2 +1,2 @@\n-old\n+new\n context\n"
	got := colorizeDiff(newStyles(store.Prefs{}), raw)
	if lines := strings.Split(got, "\n"); len(lines) != 6 {
		t.Errorf("colorizeDiff() produced %d lines, want 6:\n%q", len(lines), got)
	}
	for _, want := range []string{"-old", "+new", " context", "@@ -1,2 +1,2 @@"} {
		if !strings.Contains(got, want) {
			t.Errorf("colorizeDiff() dropped or altered %q:\n%q", want, got)
		}
	}
}

// A row is built from independently styled cells, and each one closes with a
// full SGR reset — which switches the band's background off partway along the
// line. Without re-arming it, the highlight covers the branch name, skips the
// middle of the row, and reappears only in the trailing pad. The band's own
// sequence is empty under a test's color profile, so this drives the pure half
// with a real one.
func TestReopenAfterResetsKeepsTheBandUnbroken(t *testing.T) {
	const open = "\x1b[1;48;5;236m"
	row := "\x1b[38;5;245mabc-1-perf" + ansiReset + "   \x1b[38;5;170m⚠ 2 unmergeable" + ansiReset

	got := reopenAfterResets(row, open)
	if n := strings.Count(got, ansiReset+open); n != 2 {
		t.Errorf("re-armed the band %d times, want once per reset (2):\n%q", n, got)
	}
	// Every gap between styled cells is now inside the band.
	if !strings.Contains(got, ansiReset+open+"   \x1b[38;5;170m") {
		t.Errorf("the gap before the unmergeable marker is still outside the band:\n%q", got)
	}
	if lipgloss.Width(got) != lipgloss.Width(row) {
		t.Errorf("re-arming changed the row's width: %d -> %d", lipgloss.Width(row), lipgloss.Width(got))
	}
}

func TestReopenAfterResetsIsANoOpWithoutColor(t *testing.T) {
	row := "plain row" + ansiReset
	if got := reopenAfterResets(row, ""); got != row {
		t.Errorf("no color profile: row was rewritten to %q", got)
	}
}

// --- the ? help overlay -----------------------------------------------------

// The key table is generated from the live keymap, so it can never drift from
// what the keys actually do — including after an area-12 rebind.
func TestHelpEntriesComeFromTheKeymap(t *testing.T) {
	got := helpEntries(Keymap{
		"x":     ActionRefresh, // rebound away from the default r
		"j":     ActionMoveDown,
		"down":  ActionMoveDown,
		"@":     ActionHelp,
		"ctrl+": ActionQuit,
	})

	byWhat := make(map[string]string, len(got))
	for _, e := range got {
		byWhat[e.what] = e.keys
	}
	if keys := byWhat["refresh from git"]; keys != "x" {
		t.Errorf("refresh keys = %q, want the rebound x", keys)
	}
	if keys := byWhat["move down"]; keys != "j / ↓" {
		t.Errorf("move down keys = %q, want both, arrow labelled", keys)
	}
	// An action the map does not bind must not appear at all.
	if _, ok := byWhat["fetch, then refresh"]; ok {
		t.Error("an unbound action was listed")
	}
}

// The nine accelerators are one idea, not nine rows.
func TestHelpCollapsesThePickTargetFamily(t *testing.T) {
	got := helpEntries(DefaultPairingKeys())

	var picks int
	for _, e := range got {
		if strings.Contains(e.what, "Nth configured target") {
			picks++
			if e.keys != "1–9" {
				t.Errorf("accelerator keys = %q, want the range 1–9", e.keys)
			}
		}
	}
	if picks != 1 {
		t.Errorf("pick-target rows = %d, want exactly 1", picks)
	}
}

// An action with no wording yet must be visible, not a blank row.
func TestHelpFallsBackToTheActionName(t *testing.T) {
	got := helpEntries(Keymap{"z": Action("not_written_yet")})
	if len(got) != 1 || got[0].what != "not_written_yet" {
		t.Errorf("unworded action rendered as %+v", got)
	}
}

func TestHelpOpensAndAnyKeyCloses(t *testing.T) {
	m := newModel()
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = next.(Model)
	if !m.showHelp {
		t.Fatal("? did not open the help")
	}
	if !strings.Contains(m.View(), "Glyphs") {
		t.Errorf("help view is missing the glyph legend:\n%s", m.View())
	}

	// The key that closes it must not also act on the screen underneath.
	before := m.cursor
	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = next.(Model)
	if m.showHelp {
		t.Error("a key press did not close the help")
	}
	if m.cursor != before {
		t.Errorf("the closing key also moved the cursor: %d -> %d", before, m.cursor)
	}
}

func TestHelpQuitsOnCtrlC(t *testing.T) {
	m := newModel()
	m.showHelp = true
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c in the help returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("ctrl+c in the help did not quit")
	}
}

// The help describes where you are, not a catalogue of everything Drift does.
func TestHelpReflectsTheScreenItWasOpenedFrom(t *testing.T) {
	m := branchDiffModel("flow.uwe")
	next, _ := m.dispatch(ActionToggleExpand)
	next, _ = next.(Model).handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = next.(Model)
	m.width = 110

	out := m.View()
	if !strings.Contains(out, "Keys — diff panel") {
		t.Errorf("help did not name the diff panel:\n%s", out)
	}
	if !strings.Contains(out, "declare this file unmergeable") {
		t.Errorf("help omitted the diff panel's own action:\n%s", out)
	}
	// Scrolling is deliberately unbound so it reaches the viewport; the help has
	// to say so anyway, or it would claim the panel cannot scroll.
	if !strings.Contains(out, "scroll the diff") {
		t.Errorf("help omitted scrolling:\n%s", out)
	}
	if strings.Contains(out, "add a ticket") {
		t.Errorf("help listed a dashboard-only action:\n%s", out)
	}
}

// The legend has to teach the real thing: color *is* the signal in the status
// cluster (DESIGN.md §1), so a glyph explained in the wrong color explains the
// wrong glyph. A test's profile has no color, so this pins the wiring — that
// each glyph is rendered through its own role's style and not re-wrapped in one
// flat color by the view — rather than the pixels.
func TestGlyphLegendUsesEachSignalsRealStyle(t *testing.T) {
	m := newModel()
	s := m.styles
	want := []string{
		s.ticket.Render("▸ / ▾"),
		s.behind.Render("↓N"),
		s.ahead.Render("↑N"),
		s.dirty.Render("⇡"),
		s.help.Render("⊘"),
		s.dirty.Render("●"),
		s.marker.Render("▸"),
		s.unmerge.Render("⚠ N unmergeable"),
	}

	got := m.glyphLegend()
	if len(got) != len(want) {
		t.Fatalf("legend has %d rows, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].keys != w {
			t.Errorf("legend row %d = %q, want it rendered in its own role's style (%q)", i, got[i].keys, w)
		}
		if got[i].what == "" {
			t.Errorf("legend row %d has no explanation", i)
		}
	}
}

// The glyph column holds multi-byte runes and arrives pre-styled, so the column
// has to be measured by display width, not byte length — otherwise every
// description in the legend starts at a different column.
func TestHelpColumnsAlignAcrossGlyphsAndKeys(t *testing.T) {
	m := newModel()
	m.width = 110
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})

	var starts []int
	for _, line := range strings.Split(next.(Model).View(), "\n") {
		for _, what := range []string{"move up", "ticket collapsed", "uncommitted changes", "the branch you have"} {
			if idx := strings.Index(line, what); idx >= 0 {
				starts = append(starts, lipgloss.Width(line[:idx]))
			}
		}
	}
	if len(starts) < 4 {
		t.Fatalf("expected to find 4 description rows, found %d", len(starts))
	}
	for _, at := range starts[1:] {
		if at != starts[0] {
			t.Errorf("description columns misaligned: %v", starts)
			break
		}
	}
}

// --- local-only changes (area 6) -------------------------------------------

// errBoom stands in for any failing git call.
var errBoom = errors.New("boom")

// localModel is the manager with a held set already landed, so the interaction
// tests never shell out.
func localModel(entries ...heldPath) Model {
	m := newModel()
	m.screen = screenLocalOnly
	m.local = localOnlyState{entries: entries, loaded: true}
	return m
}

func sampleHeld() []heldPath {
	return []heldPath{
		{path: "app.yml", tracked: true, note: "debug log level"},
		{path: "docker-compose.override.yml"},
	}
}

func TestDefaultLocalOnlyKeysCoverTable(t *testing.T) {
	k := DefaultLocalOnlyKeys()
	want := map[string]Action{
		"j": ActionMoveDown, "k": ActionMoveUp,
		"a": ActionHoldLocal, "d": ActionRelease, "n": ActionEditNote,
		"r": ActionRefresh, "esc": ActionCancel, "?": ActionHelp,
		"q": ActionQuit, "ctrl+c": ActionQuit,
	}
	for key, action := range want {
		if got := k[key]; got != action {
			t.Errorf("key %q = %q, want %q", key, got, action)
		}
	}
	// r must not release: a reflexive refresh from the dashboard would otherwise
	// silently drop a hold.
	if k["r"] == ActionRelease {
		t.Error("r is bound to release, where the rest of Drift means refresh")
	}
}

// Holding is its own action, not ActionAdd reused: the help table is generated
// per action, so a reused one would have to describe itself as both "add a
// ticket" and "hold a change".
func TestHoldIsItsOwnAction(t *testing.T) {
	if actionText[ActionHoldLocal] == actionText[ActionAdd] {
		t.Error("holding and adding a ticket share their help wording")
	}
	entries := helpEntries(DefaultLocalOnlyKeys())
	for _, e := range entries {
		if e.what == actionText[ActionAdd] {
			t.Errorf("the local-only help offers %q, which belongs to the dashboard", e.what)
		}
	}
}

// The manager's own screen: its keys and its glyphs, not the dashboard's.
func TestHelpOnTheManagerIsItsOwn(t *testing.T) {
	m := localModel(sampleHeld()...)
	m.showHelp = true
	view := m.View()

	if !strings.Contains(view, "local-only changes") {
		t.Error("the help does not name the screen it is for")
	}
	if !strings.Contains(view, "skip-worktree") {
		t.Error("the glyph legend does not explain the tracked hold")
	}
	if strings.Contains(view, "unmergeable") {
		t.Error("the dashboard's glyph legend leaked onto the manager's help")
	}
}

func TestReleaseRoutesByPrimitive(t *testing.T) {
	m := localModel(sampleHeld()...)

	// The tracked entry first.
	next, cmd := m.dispatch(ActionRelease)
	if cmd == nil {
		t.Fatal("release issued no command")
	}
	msg := cmd().(localHoldMsg)
	if msg.path != "app.yml" || msg.hold {
		t.Errorf("release msg = %+v, want a release of app.yml", msg)
	}
	if next.(Model).notice == "" {
		t.Error("release said nothing about what it was doing")
	}

	// Releasing needs no confirmation — nothing is destroyed, so the model must
	// not divert to a confirm screen.
	if s := next.(Model).screen; s != screenLocalOnly {
		t.Errorf("screen = %v, want to stay on the manager", s)
	}
}

// A hold or release re-reads the held set from git rather than assuming what
// Drift's own write achieved — the same rule the declare flow follows.
func TestHoldRereadsFromGit(t *testing.T) {
	m := localModel(sampleHeld()...)
	next, cmd := m.applyLocalHold(localHoldMsg{path: "scratch.md", hold: true})
	if cmd == nil {
		t.Fatal("a completed hold did not re-read the held set")
	}
	if next.notice == "" {
		t.Error("a completed hold reported nothing")
	}
}

func TestApplyLocalHoldSurfacesAFailure(t *testing.T) {
	m := localModel()
	next, cmd := m.applyLocalHold(localHoldMsg{path: "app.yml", hold: true, err: errBoom})
	if cmd != nil {
		t.Error("a failed hold still re-read the held set")
	}
	if !strings.Contains(next.notice, "app.yml") {
		t.Errorf("notice = %q, want it to name the path that failed", next.notice)
	}
}

// The held set comes from git; the store contributes only the notes, and loses
// the ones git no longer backs.
func TestApplyLocalOnlyJoinsNotesAndPrunesOrphans(t *testing.T) {
	m := localModel()
	m.store.LocalOnly = []store.LocalOnly{
		{Path: "app.yml", Note: "debug log level"},
		{Path: "released-elsewhere.yml", Note: "stale"},
	}

	next, cmd := m.applyLocalOnly(localOnlyMsg{held: []heldPath{{path: "app.yml", tracked: true}}})
	if len(next.local.entries) != 1 || next.local.entries[0].note != "debug log level" {
		t.Errorf("entries = %+v, want the stored note joined on", next.local.entries)
	}
	if len(next.store.LocalOnly) != 1 {
		t.Errorf("store = %+v, want the orphaned note dropped", next.store.LocalOnly)
	}
	if cmd == nil {
		t.Error("the prune was not persisted")
	}
}

// A load that lands after the user left the manager must not repaint it.
func TestApplyLocalOnlyIgnoresAStaleLoad(t *testing.T) {
	m := newModel() // on the dashboard
	next, _ := m.applyLocalOnly(localOnlyMsg{held: []heldPath{{path: "app.yml"}}})
	if len(next.local.entries) != 0 {
		t.Error("a load landed on a screen the user had already left")
	}
}

// Offering "hold this" for something already held would be a lie.
func TestCandidatesExcludeWhatIsAlreadyHeld(t *testing.T) {
	m := localModel(sampleHeld()...)
	m.local.add = addLocalState{open: true}

	next := m.applyLocalCandidates(localCandidatesMsg{changes: []git.WorkingChange{
		{Path: "app.yml", Tracked: true, Staged: true},
		{Path: "scratch.md"},
	}})
	if len(next.local.add.candidates) != 1 || next.local.add.candidates[0].path != "scratch.md" {
		t.Errorf("candidates = %+v, want the already-held path filtered out", next.local.add.candidates)
	}
}

// skip-worktree hides the working tree, not the index — so holding a staged
// change would look like protection and give none. It is refused, with the fix.
func TestStagedCandidateIsRefused(t *testing.T) {
	m := localModel()
	m.local.add = addLocalState{
		open:       true,
		loaded:     true,
		candidates: []localCandidate{{path: "app.yml", tracked: true, staged: true}},
	}

	next, cmd := m.dispatch(ActionConfirm)
	if cmd != nil {
		t.Error("a staged change was held anyway")
	}
	nm := next.(Model)
	if !nm.local.add.open {
		t.Error("the picker closed on a refused hold, hiding the reason")
	}
	if !strings.Contains(nm.notice, "unstage") {
		t.Errorf("notice = %q, want it to say how to fix this", nm.notice)
	}
}

func TestHoldRoutesUntrackedToExclude(t *testing.T) {
	m := localModel()
	m.local.add = addLocalState{
		open:       true,
		loaded:     true,
		candidates: []localCandidate{{path: "scratch.md"}},
	}

	next, cmd := m.dispatch(ActionConfirm)
	if cmd == nil {
		t.Fatal("confirm issued no command")
	}
	if msg := cmd().(localHoldMsg); msg.path != "scratch.md" || !msg.hold {
		t.Errorf("hold msg = %+v, want a hold of scratch.md", msg)
	}
	if next.(Model).local.add.open {
		t.Error("the picker stayed open after a hold")
	}
}

// The note editor is a text field: unbound keys type into it rather than
// reaching the list underneath.
func TestNoteEditorTakesKeystrokes(t *testing.T) {
	m := localModel(sampleHeld()...)
	next, _ := m.dispatch(ActionEditNote)
	nm := next.(Model)
	if !nm.local.note.open || nm.local.note.path != "app.yml" {
		t.Fatalf("note editor = %+v, want it open on the selected path", nm.local.note)
	}
	if nm.input.Value() != "debug log level" {
		t.Errorf("input = %q, want it seeded with the existing note", nm.input.Value())
	}

	// "d" would release on the list; here it must be a keystroke.
	typed, _ := nm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	tm := typed.(Model)
	if !tm.local.note.open {
		t.Error("typing in the note editor triggered a list action")
	}
	if !strings.HasSuffix(tm.input.Value(), "d") {
		t.Errorf("input = %q, want the keystroke to have landed in the field", tm.input.Value())
	}
}

func TestNoteEditorSavesAndClears(t *testing.T) {
	m := localModel(sampleHeld()...)
	next, _ := m.dispatch(ActionEditNote)
	nm := next.(Model)
	nm.input.SetValue("  why it's here  ")

	saved, cmd := nm.dispatch(ActionConfirm)
	sm := saved.(Model)
	if cmd == nil {
		t.Error("the note was not persisted")
	}
	if sm.local.note.open {
		t.Error("the editor stayed open after saving")
	}
	if sm.store.LocalOnlyNote("app.yml") != "why it's here" {
		t.Errorf("stored note = %q, want it trimmed and saved", sm.store.LocalOnlyNote("app.yml"))
	}
	// The row updates in place, so the list reflects the edit without a reload.
	if sm.local.entries[0].note != "why it's here" {
		t.Errorf("row note = %q, want the list updated too", sm.local.entries[0].note)
	}

	// esc keeps the previous note.
	reopened, _ := sm.dispatch(ActionEditNote)
	cancelled, _ := reopened.(Model).dispatch(ActionCancel)
	if cancelled.(Model).store.LocalOnlyNote("app.yml") != "why it's here" {
		t.Error("cancelling the editor changed the stored note")
	}
}

// The scope is a property of the primitives, not a limitation — and the UI must
// not imply a hold is per-branch (CONTEXT.md).
func TestManagerStatesItsScope(t *testing.T) {
	view := localModel(sampleHeld()...).View()
	if !strings.Contains(view, "every branch") {
		t.Error("the manager does not say a hold applies to every branch")
	}
	for _, want := range []string{"app.yml", "skip-worktree", "debug log level", "info/exclude"} {
		if !strings.Contains(view, want) {
			t.Errorf("the list is missing %q", want)
		}
	}
}

func TestManagerEmptyStateTeaches(t *testing.T) {
	view := localModel().View()
	if !strings.Contains(view, "Nothing held yet") {
		t.Error("the empty manager does not say so")
	}
	if !strings.Contains(view, "a to hold") {
		t.Error("the empty manager does not teach how to seed it")
	}
}

func TestManagerCancelReturnsHome(t *testing.T) {
	m := localModel(sampleHeld()...)
	next, _ := m.dispatch(ActionCancel)
	nm := next.(Model)
	if nm.screen != screenDashboard {
		t.Errorf("screen = %v, want the dashboard", nm.screen)
	}
	if len(nm.local.entries) != 0 {
		t.Error("the manager kept its state, so a stale set could be shown on the next visit")
	}
}

// The cursor must survive a reload that shortens the list.
func TestManagerCursorSurvivesAShorterList(t *testing.T) {
	m := localModel(sampleHeld()...)
	m.local.cursor = 1
	next, _ := m.applyLocalOnly(localOnlyMsg{held: []heldPath{{path: "app.yml", tracked: true}}})
	if next.local.cursor != 0 {
		t.Errorf("cursor = %d, want it clamped into the shorter list", next.local.cursor)
	}
}

// The whole loop against a real repo: open the manager, hold one change of each
// kind, and confirm git itself stops reporting them — which is the promise, and
// the only thing worth asserting end to end.
func TestLocalOnlyLoopAgainstARealRepo(t *testing.T) {
	dir := newTestRepo(t)
	writeCommit(t, dir, "app.yml", "level: info\n")
	if err := os.WriteFile(filepath.Join(dir, "app.yml"), []byte("level: debug\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scratch.md"), []byte("notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := git.New(dir)
	m := New(repo, samplePaths(), sampleConfig(), store.Store{}, store.Prefs{})
	m.loading = false

	next, cmd := m.dispatch(ActionLocalOnly)
	m = next.(Model)
	m, _ = m.applyLocalOnly(cmd().(localOnlyMsg))
	if len(m.local.entries) != 0 {
		t.Fatalf("a fresh repo reported %+v held", m.local.entries)
	}

	// Hold both candidates, one of each kind, through the real commands.
	next, cmd = m.dispatch(ActionHoldLocal)
	m = next.(Model).applyLocalCandidates(cmd().(localCandidatesMsg))
	if len(m.local.add.candidates) != 2 {
		t.Fatalf("candidates = %+v, want the tracked edit and the untracked file", m.local.add.candidates)
	}
	for _, c := range m.local.add.candidates {
		if msg := holdLocalCmd(repo, c.path, c.tracked)().(localHoldMsg); msg.err != nil {
			t.Fatalf("hold %s: %v", c.path, msg.err)
		}
	}

	// Git's own answer is the one that counts.
	dirty, err := repo.IsDirty(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if dirty {
		t.Error("git still reports the working tree dirty — the holds did not take")
	}

	m.local.add = addLocalState{}
	m, _ = m.applyLocalOnly(loadLocalOnlyCmd(repo)().(localOnlyMsg))
	if len(m.local.entries) != 2 {
		t.Fatalf("entries = %+v, want both holds listed", m.local.entries)
	}
	byPath := map[string]heldPath{}
	for _, h := range m.local.entries {
		byPath[h.path] = h
	}
	if !byPath["app.yml"].tracked {
		t.Error("app.yml is listed as untracked, so release would use the wrong primitive")
	}
	if byPath["scratch.md"].tracked {
		t.Error("scratch.md is listed as tracked")
	}

	// Releasing hands both changes back, unharmed.
	for _, h := range m.local.entries {
		if msg := releaseLocalCmd(repo, h.path, h.tracked)().(localHoldMsg); msg.err != nil {
			t.Fatalf("release %s: %v", h.path, msg.err)
		}
	}
	if dirty, _ = repo.IsDirty(context.Background()); !dirty {
		t.Error("the released edits did not come back")
	}
	body, err := os.ReadFile(filepath.Join(dir, "app.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "level: debug\n" {
		t.Errorf("app.yml = %q, want the local edit intact throughout", body)
	}
}
