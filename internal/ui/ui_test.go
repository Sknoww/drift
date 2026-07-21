package ui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
		"r": ActionRefresh, "f": ActionFetch,
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

func TestDispatchUnbuiltActionsAnnounce(t *testing.T) {
	for _, a := range []Action{ActionAdd, ActionDelete, ActionLocalOnly} {
		next, cmd := newModel().dispatch(a)
		if cmd != nil {
			t.Errorf("%s: expected no command", a)
		}
		if next.(Model).notice == "" {
			t.Errorf("%s: expected a notice explaining where it's headed", a)
		}
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

// --- view -----------------------------------------------------------------

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
