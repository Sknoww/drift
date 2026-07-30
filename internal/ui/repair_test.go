package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Sknoww/drift/internal/git"
	"github.com/Sknoww/drift/internal/store"
)

// saveStateResult digs the state write out of a Cmd, following the batch it is
// bundled in — the same shape shelveResult uses for the sequence's messages.
func saveStateResult(cmd tea.Cmd) (saveStateMsg, bool) {
	switch msg := cmd().(type) {
	case saveStateMsg:
		return msg, true
	case tea.BatchMsg:
		for _, c := range msg {
			if got, ok := saveStateResult(c); ok {
				return got, true
			}
		}
	}
	return saveStateMsg{}, false
}

// Re-pairing a branch (roadmap 19b). The state 19b exists to correct: a branch
// paired to a target that is not the one it should aim at, visible on the
// dashboard and — until this shipped — correctable only by hand-editing
// state.json with Drift closed.

// repairModel is the dashboard with ABC-1 expanded and the cursor on its first
// branch row, which is the only row `p` acts on.
func repairModel() Model {
	m := newModel()
	m.width, m.height = 100, 24
	m.expanded["ABC-1"] = true
	m.cursor = 1 // ABC-1's headline is row 0; abc-1-perf is row 1
	return m
}

// --- getting there --------------------------------------------------------

func TestRepairKeyReachesTheAction(t *testing.T) {
	if got, ok := DefaultDashboardKeys().action("p"); !ok || got != ActionRepair {
		t.Errorf("dashboard p -> %q, %v; want %q", got, ok, ActionRepair)
	}
	// Same overlay, same field, different commitment: on the checklist a pick is
	// provisional until enter saves the ticket, here it is written at once. The
	// help table is generated per action, so the two have to be able to say so.
	if actionText[ActionRepair] == actionText[ActionOpenPicker] {
		t.Errorf("ActionRepair and ActionOpenPicker describe themselves identically: %q", actionText[ActionRepair])
	}
	if actionText[ActionRepair] == "" {
		t.Error("ActionRepair has no wording, so the ? overlay would show its bare name")
	}
}

func TestRepairOpensOnTheBranchsCurrentTarget(t *testing.T) {
	m := repairModel() // abc-1-perf is paired to r2perf, which is targets[0]
	next, cmd := m.dispatch(ActionRepair)
	m = next.(Model)

	if !m.repair.open {
		t.Fatal("p did not open the picker")
	}
	if cmd != nil {
		t.Error("p asked git something; the targets are already in the model")
	}
	if m.repair.branch != "abc-1-perf" || m.repair.ticketID != "ABC-1" {
		t.Errorf("picker is about %s/%s, want ABC-1/abc-1-perf", m.repair.ticketID, m.repair.branch)
	}
	if m.repair.from != "r2perf" {
		t.Errorf("from = %q, want r2perf", m.repair.from)
	}
	// It opens on the value it is changing. A picker that opened at the top would
	// make the user find their current pairing before deciding against changing it.
	if m.repair.cursor != 0 {
		t.Errorf("cursor = %d, want 0 (r2perf)", m.repair.cursor)
	}

	// And the second branch on the same ticket opens on *its* target, not the
	// first one's — the row is the subject, which is the whole of 19b's scope.
	m = repairModel()
	m.cursor = 2 // abc-1-main, paired to main == targets[1]
	next, _ = m.dispatch(ActionRepair)
	if got := next.(Model).repair.cursor; got != 1 {
		t.Errorf("cursor for abc-1-main = %d, want 1 (main)", got)
	}
}

// A pairing belongs to a branch, so a ticket headline is refused with the reason
// named — the same shape as `s` and `u` refusing on a ticket row.
func TestRepairRefusesOnATicketRow(t *testing.T) {
	m := newModel() // cursor on ABC-1's headline, nothing expanded
	next, cmd := m.dispatch(ActionRepair)
	m = next.(Model)

	if m.repair.open {
		t.Error("p opened a picker for a ticket row")
	}
	if cmd != nil {
		t.Error("p on a ticket row fired a Cmd")
	}
	if !strings.Contains(m.notice, "branch row") {
		t.Errorf("notice = %q, want it to say a pairing belongs to a branch", m.notice)
	}
}

// Reachable only from a hand-edited config — SaveConfig's validate() refuses to
// write a target-less one. Say what is missing rather than open an empty picker.
func TestRepairRefusesWithNoTargets(t *testing.T) {
	m := repairModel()
	m.cfg = store.Config{}

	next, _ := m.dispatch(ActionRepair)
	m = next.(Model)
	if m.repair.open {
		t.Error("p opened a picker with no targets to offer")
	}
	if !strings.Contains(m.notice, "no targets") || !strings.Contains(m.notice, m.paths.Config) {
		t.Errorf("notice = %q, want it to name what is missing and the file", m.notice)
	}
}

// --- the picker -----------------------------------------------------------

// It is the *same* overlay as the pairing checklist's, so it shows the same
// things: every configured target, its ref beside it so a terse key is never
// ambiguous, and the accelerator digits.
func TestRepairPickerShowsTheTargetsAndTheirRefs(t *testing.T) {
	next, _ := repairModel().dispatch(ActionRepair)
	view := next.(Model).View()

	for _, want := range []string{"abc-1-perf", "r2perf", "origin/release-to-performance", "main", "origin/main", "1 ", "2 "} {
		if !strings.Contains(view, want) {
			t.Errorf("%q is not on the picker; got:\n%s", want, view)
		}
	}
	// The overlay covers the list it was opened from, so it has to name its
	// subject — the row it is about is the row it hid.
	if strings.Contains(view, "ABC-2") {
		t.Errorf("the ticket list is still drawn behind the picker; got:\n%s", view)
	}
}

// The picker marks the target the branch aims at now — 19e's argument about its
// ref picker, applied to this one. The cursor opens on that row, but that signal
// is gone the moment the user moves, and a list of targets with nothing
// distinguishing one from another cannot say what is being changed *from*.
func TestRepairPickerMarksTheCurrentTarget(t *testing.T) {
	next, _ := repairModel().dispatch(ActionRepair) // abc-1-perf aims at r2perf
	m := next.(Model)

	line := ""
	for _, l := range strings.Split(m.View(), "\n") {
		if strings.Contains(l, "r2perf") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("r2perf is not on the picker; got:\n%s", m.View())
	}
	if !strings.Contains(line, currentRefLabel) {
		t.Errorf("the current target is not marked; its row is %q", line)
	}
	// And only it — the mark says which one you have, so a second one would say
	// nothing at all.
	for _, l := range strings.Split(m.View(), "\n") {
		if strings.Contains(l, "origin/main") && strings.Contains(l, currentRefLabel) {
			t.Errorf("a target that is not the current one is marked: %q", l)
		}
	}
}

// The mark survives a long ref, because it is what the row is *for*. Left unsized
// the ref would push it off the end for clipRow to cut — the trailing cell, which
// is the signal slice A's allocation rule exists to protect.
func TestRepairPickerKeepsTheMarkWhenTheRefOverflows(t *testing.T) {
	m := repairModel()
	m.cfg = store.Config{Targets: []store.Target{
		{Key: "r2perf", Ref: "origin/" + strings.Repeat("long-enough-to-overflow/", 8) + "tip"},
		{Key: "main", Ref: "origin/main"},
	}}
	m.targetKeyWidth = widestTargetKey(m.cfg)
	m.width = minTerminalWidth

	next, _ := m.dispatch(ActionRepair)
	view := next.(Model).View()
	if !strings.Contains(view, currentRefLabel) {
		t.Errorf("an overflowing ref cost the current-target mark; got:\n%s", view)
	}
	// The head of the ref is what survives, as everywhere else in 19.
	if !strings.Contains(view, "origin/long-enough") {
		t.Errorf("the ref's head is not on screen; got:\n%s", view)
	}
}

func TestRepairPickerCursorClamps(t *testing.T) {
	next, _ := repairModel().dispatch(ActionRepair) // two targets
	m := next.(Model)

	next, _ = m.dispatch(ActionMoveUp)
	if got := next.(Model).repair.cursor; got != 0 {
		t.Errorf("up at the top: cursor = %d, want 0", got)
	}
	for i := 0; i < 5; i++ {
		next, _ = next.(Model).dispatch(ActionMoveDown)
	}
	if got := next.(Model).repair.cursor; got != 1 {
		t.Errorf("down past the end: cursor = %d, want 1", got)
	}
}

// The picker shadows the dashboard's keymap while it is open, so `d` cannot
// delete a ticket and `u` cannot start an update from inside a choice.
func TestRepairPickerShadowsTheDashboardKeys(t *testing.T) {
	next, _ := repairModel().dispatch(ActionRepair)
	m := next.(Model)

	if got, want := m.activeKeys(), m.keys.picker; len(got) != len(want) {
		t.Errorf("active keymap has %d bindings, want the picker's %d", len(got), len(want))
	}
	for _, key := range []string{"d", "u", "s", "a", "f", "t"} {
		if _, ok := m.activeKeys().action(key); ok {
			t.Errorf("%q still acts while the picker is open", key)
		}
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if got := next.(Model); !got.repair.open || got.screen != screenDashboard {
		t.Errorf("d inside the picker left open=%v screen=%v", got.repair.open, got.screen)
	}
}

func TestRepairEscChangesNothing(t *testing.T) {
	next, _ := repairModel().dispatch(ActionRepair)
	next, _ = next.(Model).dispatch(ActionMoveDown)
	next, cmd := next.(Model).dispatch(ActionCancel)
	m := next.(Model)

	if m.repair.open {
		t.Error("esc left the picker open")
	}
	if cmd != nil {
		t.Error("esc fired a Cmd; the picker is read-only until enter")
	}
	if got := m.store.Tickets[0].Branches[0].TargetKey; got != "r2perf" {
		t.Errorf("esc changed the pairing to %q", got)
	}
}

// --- the write ------------------------------------------------------------

// enter commits, with no y/n. DESIGN.md §3 names the re-point confirmation as the
// one place a picker in Drift does not — it earned one because it re-bases every
// row's ↓behind. This re-bases one row, and that row shows its new key at once.
func TestRepairConfirmWritesThePairing(t *testing.T) {
	next, _ := repairModel().dispatch(ActionRepair)
	next, _ = next.(Model).dispatch(ActionMoveDown) // onto main
	next, cmd := next.(Model).dispatch(ActionConfirm)
	m := next.(Model)

	if m.repair.open {
		t.Error("enter left the picker open")
	}
	if got := m.store.Tickets[0].Branches[0].TargetKey; got != "main" {
		t.Errorf("abc-1-perf is paired to %q, want main", got)
	}
	// One row, and only that row.
	if got := m.store.Tickets[0].Branches[1].TargetKey; got != "main" {
		t.Errorf("abc-1-main's pairing changed to %q", got)
	}
	if got := m.store.Tickets[1].Branches[0].TargetKey; got != "main" {
		t.Errorf("ABC-2's pairing changed to %q", got)
	}
	if cmd == nil {
		t.Fatal("enter fired no Cmd, so nothing was persisted or re-swept")
	}

	// The row is the feedback, and it is permanent where a notice is transient —
	// the sweep this fires clears the status line the moment it lands.
	if m.notice != "" {
		t.Errorf("notice = %q, want none: the row already shows the new target", m.notice)
	}
	m.expanded["ABC-1"] = true
	if view := m.View(); !strings.Contains(view, "main") {
		t.Errorf("the row does not show the new target; got:\n%s", view)
	}
}

// A pairing decides which ref the branch is measured against, so the numbers on
// the row are about the old target until a sweep says otherwise — the rule areas
// 5 and 6 landed on, one field along.
func TestRepairResweepsAfterTheWrite(t *testing.T) {
	next, _ := repairModel().dispatch(ActionRepair)
	m := next.(Model)
	before := m.sweepID

	next, cmd := m.dispatch(ActionMoveDown)
	next, cmd = next.(Model).dispatch(ActionConfirm)
	m = next.(Model)

	if !m.loading {
		t.Error("no sweep was started after the write")
	}
	if m.sweepID == before {
		t.Error("the sweep id did not advance, so an in-flight sweep could clobber the new pairing")
	}
	if cmd == nil {
		t.Fatal("enter fired no Cmd")
	}
}

// The accelerators are live *inside* the picker, because the picker draws them.
// Until 19b shared the body they only worked after esc-ing back to the checklist,
// which made every digit on screen a key that did nothing where it was shown.
func TestRepairAcceleratorAssignsTheNthTarget(t *testing.T) {
	next, _ := repairModel().dispatch(ActionRepair)
	next, cmd := next.(Model).dispatch(ActionPickTarget(2)) // main
	m := next.(Model)

	if m.repair.open {
		t.Error("the accelerator left the picker open")
	}
	if got := m.store.Tickets[0].Branches[0].TargetKey; got != "main" {
		t.Errorf("2 paired abc-1-perf to %q, want main", got)
	}
	if cmd == nil {
		t.Fatal("the accelerator fired no Cmd")
	}
}

// A digit past the configured targets refuses and says so — and the picker stays
// open, because the user pressed a key meaning "that one" and closing on it would
// read as having chosen something.
func TestRepairAcceleratorPastTheTargetsKeepsThePickerOpen(t *testing.T) {
	next, _ := repairModel().dispatch(ActionRepair)
	next, cmd := next.(Model).dispatch(ActionPickTarget(7))
	m := next.(Model)

	if !m.repair.open {
		t.Error("a digit with no target behind it closed the picker")
	}
	if cmd != nil {
		t.Error("an empty slot fired a Cmd")
	}
	if !strings.Contains(m.notice, "no target in that slot") {
		t.Errorf("notice = %q, want it to say the slot is empty", m.notice)
	}
	if got := m.store.Tickets[0].Branches[0].TargetKey; got != "r2perf" {
		t.Errorf("an empty slot changed the pairing to %q", got)
	}
}

// Picking the target the branch already aims at is said out loud rather than
// written: it is the one outcome where "it worked" and "nothing happened" leave
// the row identical, so the screen has to be what tells them apart (19e's rule).
func TestRepairToTheSameTargetIsANoOp(t *testing.T) {
	next, _ := repairModel().dispatch(ActionRepair)
	next, cmd := next.(Model).dispatch(ActionConfirm) // cursor 0 is r2perf, its own target
	m := next.(Model)

	if m.repair.open {
		t.Error("the no-op left the picker open")
	}
	if cmd != nil {
		t.Error("re-pairing to the same target fired a Cmd")
	}
	if !strings.Contains(m.notice, "already paired to") {
		t.Errorf("notice = %q, want it to say nothing changed", m.notice)
	}
}

// A selection gone stale is stated, never written onto something that is gone.
// Only reachable if the store changed under the overlay, which is exactly why the
// picker holds a ticket ID and a branch name rather than a row index.
func TestRepairOnAVanishedBranchSaysSo(t *testing.T) {
	next, _ := repairModel().dispatch(ActionRepair)
	m := next.(Model)
	m.store = store.Store{Tickets: []store.Ticket{{ID: "ABC-1"}}} // the branch is gone

	next, cmd := m.dispatch(ActionMoveDown)
	next, cmd = next.(Model).dispatch(ActionConfirm)
	m = next.(Model)

	if cmd != nil {
		t.Error("a stale selection still wrote something")
	}
	if !strings.Contains(m.notice, "abc-1-perf") || !strings.Contains(m.notice, "nothing was changed") {
		t.Errorf("notice = %q, want it to name the branch and say nothing changed", m.notice)
	}
}

// --- the dashboard's own affordances -------------------------------------

// The help line offers `p` on a terminal wide enough to hold it, and the `?`
// overlay — where everything elided lives — names it at every width. `p` sits
// behind the sweep in the lead, so it is elided before `r` and `f` are: a
// correction made once loses its slot before the keys pressed all day do.
func TestDashboardAdvertisesRepair(t *testing.T) {
	m := repairModel()
	m.width = 160
	if view := m.View(); !strings.Contains(view, "p re-pair") {
		t.Errorf("the help line does not offer p; got:\n%s", view)
	}
	m.width = 120
	view := m.View()
	if strings.Contains(view, "p re-pair") {
		t.Errorf("p survived at 120 columns, where r/f are meant to outrank it; got:\n%s", view)
	}
	if !strings.Contains(view, "r refresh") || !strings.Contains(view, "f fetch") {
		t.Errorf("adding p cost the sweep its slot at 120 columns; got:\n%s", view)
	}
	m.width = 100

	m.showHelp = true
	overlay := m.View()
	if !strings.Contains(overlay, actionText[ActionRepair]) {
		t.Errorf("the ? overlay does not describe p; got:\n%s", overlay)
	}
	// Generated from the live keymap, so the key beside it is the one really bound.
	if !strings.Contains(overlay, "p") {
		t.Errorf("the ? overlay does not name the key; got:\n%s", overlay)
	}
}

// The picker is a momentary choice step and carries its own one-line help, the
// same as the target picker, the declare overlay and the hold picker — so it
// binds no `?`, and there is no overlay to be opened over it.
func TestRepairPickerBindsNoHelp(t *testing.T) {
	next, _ := repairModel().dispatch(ActionRepair)
	m := next.(Model)

	if _, ok := m.activeKeys().action("?"); ok {
		t.Error("the picker binds ?, which the other choice overlays deliberately do not")
	}
	if view := m.View(); !strings.Contains(view, "enter re-pair") || !strings.Contains(view, "esc back") {
		t.Errorf("the picker's own help line does not say how to commit or leave; got:\n%s", view)
	}
}

// The overlay is drawn inside the frame it was opened over, whatever the terminal
// and however many targets a config names — a windowed list still has to be
// clipped in both axes, and 19e's picker is where that was last proved.
func TestRepairPickerBoundsTheFrame(t *testing.T) {
	const height = 24

	targets := make([]store.Target, 400)
	for i := range targets {
		targets[i] = store.Target{
			Key: fmt.Sprintf("release-%04d-long-enough-to-crowd-the-row", i),
			Ref: fmt.Sprintf("origin/fix/TEAM-%04d-a-fairly-long-branch-name/mvp-3", i),
		}
	}

	for _, width := range []int{minTerminalWidth, 80, 100} {
		base := repairModel()
		base.cfg = store.Config{Targets: targets}
		base.targetKeyWidth = widestTargetKey(base.cfg)
		base.width, base.height = width, height

		next, _ := base.dispatch(ActionRepair)
		m := next.(Model)
		for _, cursor := range []int{0, 200, len(targets) - 1} {
			m.repair.cursor = cursor
			view := m.View()
			if lines := strings.Count(view, "\n") + 1; lines > height {
				t.Fatalf("width %d cursor %d: frame is %d lines on a %d-line terminal:\n%s",
					width, cursor, lines, height, view)
			}
			for _, line := range strings.Split(view, "\n") {
				if w := lipgloss.Width(line); w > width {
					t.Fatalf("width %d cursor %d: a line is %d cells: %q", width, cursor, w, line)
				}
			}
			// The selected row is on screen — the acceptance test area 14 set.
			if head := fmt.Sprintf("release-%04d", cursor); !strings.Contains(view, head) {
				t.Fatalf("width %d cursor %d: the selected target is not on screen:\n%s", width, cursor, view)
			}
		}
	}
}

// --- the whole thing, against a real repository --------------------------

// End to end through the model rather than around it: the wiring — the Cmd,
// store.SaveState resolving the git dir for itself, and the re-sweep — is what a
// unit test over a hand-built store cannot check. The state on disk is what a
// user's next run of Drift would load.
func TestRepairEndToEndRewritesTheState(t *testing.T) {
	dir := t.TempDir()
	rungit(t, dir, "init", "--quiet", dir)

	repo := git.New(dir)
	paths, err := store.Resolve(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}

	// The state 19b corrects: a branch aimed at the wrong one of two targets.
	st := store.Store{Tickets: []store.Ticket{{ID: "ABC-1", Branches: []store.TicketBranch{
		{Branch: "abc-1-perf", TargetKey: "r2perf"},
	}}}}
	if err := store.SaveState(context.Background(), repo, st); err != nil {
		t.Fatal(err)
	}

	m := New(repo, paths, sampleConfig(), st, store.Prefs{})
	m.loading = false
	m.width, m.height = 100, 24
	m.expanded["ABC-1"] = true
	m.cursor = 1

	next, _ := m.dispatch(ActionRepair)
	m = next.(Model)
	if !strings.Contains(m.View(), "abc-1-perf") {
		t.Errorf("the picker does not name the branch it is about; got:\n%s", m.View())
	}

	next, _ = m.dispatch(ActionMoveDown) // onto main
	next, cmd := next.(Model).dispatch(ActionConfirm)
	m = next.(Model)
	if cmd == nil {
		t.Fatal("enter fired no Cmd")
	}
	// The save is batched with the spinner tick and the sweep, so a plain type
	// assertion would only ever see the batch.
	msg, ok := saveStateResult(cmd)
	if !ok {
		t.Fatal("enter's Cmd never saved the state")
	}
	if msg.err != nil {
		t.Fatal(msg.err)
	}

	back, _, err := store.LoadState(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Tickets) != 1 || len(back.Tickets[0].Branches) != 1 {
		t.Fatalf("state.json came back as %+v", back)
	}
	if got := back.Tickets[0].Branches[0].TargetKey; got != "main" {
		t.Errorf("state.json holds targetKey = %q, wanted main", got)
	}
	if got := back.Tickets[0].Branches[0].Branch; got != "abc-1-perf" {
		t.Errorf("the branch name changed to %q", got)
	}
}
