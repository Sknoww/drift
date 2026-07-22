package ui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"drift/internal/git"
	"drift/internal/store"
)

// --- model fixtures -------------------------------------------------------

func sampleConfig() store.Config {
	return store.Config{Targets: []store.Target{
		{Key: "r2perf", Ref: "origin/release-to-performance"},
		{Key: "main", Ref: "origin/main"},
	}}
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
	m := New(git.New(t_nowhere), sampleConfig(), sampleStore())
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
		"l": ActionLocalOnly, "q": ActionQuit, "ctrl+c": ActionQuit,
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

func TestDispatchLocalOnlyAnnounces(t *testing.T) {
	// local-only is still ahead (area 6); it must say where it's headed rather
	// than do nothing.
	next, cmd := newModel().dispatch(ActionLocalOnly)
	if cmd != nil {
		t.Error("local-only: expected no command")
	}
	if next.(Model).notice == "" {
		t.Error("local-only: expected a notice explaining where it's headed")
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
	m := New(git.New(t_nowhere), sampleConfig(), store.Store{})
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

func TestViewRendersUnknownTarget(t *testing.T) {
	st := store.Store{Tickets: []store.Ticket{
		{ID: "X", Branches: []store.TicketBranch{{Branch: "b", TargetKey: "gone"}}},
	}}
	m := New(git.New(t_nowhere), sampleConfig(), st)
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

// --- test-repo helpers ----------------------------------------------------

func rungit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
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
	if got := strings.Join(feat.unmergeable, ","); got != "flow.uwe" {
		t.Errorf("feature unmergeable = %q, want %q", got, "flow.uwe")
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
	if strings.Join(got, ",") != "flow.uwe" {
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
	if strings.Join(got, ",") != "flow.uwe" {
		t.Errorf("config-glob detection = %v, want [flow.uwe]", got)
	}
}

// branchDiffModel returns a dashboard with ABC-1 expanded, the cursor on its
// first branch, and that branch carrying `files` as unmergeable collisions.
func branchDiffModel(files ...string) Model {
	m := newModel()
	m.expanded["ABC-1"] = true
	m.cursor = 1 // ABC-1 headline is row 0; its first branch is row 1
	m.status[statusKey("ABC-1", "abc-1-perf")] = branchStatus{known: true, behind: 2, unmergeable: files}
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
	if strings.Join(mm.diff.files, ",") != "flow.uwe,scene.unity" {
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

func TestDiffFileCyclingClamps(t *testing.T) {
	m := branchDiffModel("a.uwe", "b.uwe")
	next, _ := m.dispatch(ActionToggleExpand)
	m = next.(Model)

	// prev at the first file is a no-op.
	back, _ := m.dispatchDiff(ActionPrevFile)
	if back.(Model).diff.cursor != 0 {
		t.Errorf("prev at first file moved: cursor = %d", back.(Model).diff.cursor)
	}
	// next advances, then clamps at the last file.
	fwd, _ := m.dispatchDiff(ActionNextFile)
	m = fwd.(Model)
	if m.diff.cursor != 1 {
		t.Fatalf("next: cursor = %d, want 1", m.diff.cursor)
	}
	end, _ := m.dispatchDiff(ActionNextFile)
	if end.(Model).diff.cursor != 1 {
		t.Errorf("next past last file moved: cursor = %d, want 1", end.(Model).diff.cursor)
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
