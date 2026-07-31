package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Sknoww/drift/internal/git"
	"github.com/Sknoww/drift/internal/store"
)

// The targets screen: every configured target, and the ref behind it (roadmap
// 19e).
//
// It exists because a target's Ref was write-once and invisible. It is written
// in exactly one place — the first-run wizard — and until now it was *rendered*
// in exactly one, the wizard's own picker row. Every screen the user actually
// lives in shows Target.Key: the dashboard's branch rows, the pairing
// checklist's assignment cell, the status cluster's "unknown target" warning.
// So a target pointing at the wrong branch looked correct everywhere, because
// the key was correct — that is exactly how v0.3.0 published a merge of a
// colleague's ticket branch that happened to be named `…/mvp-3` under a target
// keyed `mvp-3`.
//
// Showing and correcting were built as separate jobs (the roadmap's own split),
// and showing came first because it is the half that turns an invisible field
// into a visible one. Correcting is `e`: a picker of the repo's own remote refs,
// a y/n confirmation, and a write. The cursor was here from the first version
// precisely so this could be an addition rather than a retrofit — the argument
// area 3 made for named actions, one screen later.
//
// The *list itself* asks git nothing, and that is a finding rather than a
// shortcut. The obvious addition — flag a target whose ref no longer resolves —
// would not have caught the incident: the wrong ref was a real branch that
// resolved perfectly. The picker asks git, because choosing needs real refs to
// choose from; the list does not, because its subject is what the config says.

// repointState is the open re-point flow (19e): the ref picker over the targets
// list, then the confirmation between a picked ref and config.json being
// rewritten.
//
// key is the target being re-pointed rather than an index, so a list rebuilt
// under the flow (a re-point landing while another is open cannot happen, but a
// config folded in by an earlier one can) resolves to the same target or to
// none. Its Key never changes — only Ref does (store.SetTargetRef).
type repointState struct {
	open bool
	key  string // the target being re-pointed
	from string // the ref it points at now: the picker marks it, the confirm names it

	refs   []git.RemoteBranch
	loaded bool        // false until the ref scan answers
	cursor int         // index into visible(), not into refs
	filter filterState // type-to-filter, because this is the wizard's list again

	// now is the clock the age column is rendered against, captured when the
	// picker opens. Fixed rather than ticking, for the wizard's reason: this is a
	// screen you are on for seconds, so an age that moved would be motion with
	// nothing to say — and a fixed clock keeps the column assertable in a test.
	now time.Time

	confirm bool   // the picked ref is awaiting y/n
	to      string // the picked ref, meaningful only while confirm is open
}

// openTargets shows the configured targets. No Cmd: the config is already in
// the model, so unlike every other screen there is nothing to load.
func (m Model) openTargets() (tea.Model, tea.Cmd) {
	m.screen = screenTargets
	m.targetsCur = 0
	m.repoint = repointState{}
	m.notice = ""
	return m, nil
}

// dispatchTargets runs one named action on the targets screen, delegating to
// whichever step of the re-point flow is open over the list — the same shape the
// local-only manager uses for its two overlays.
func (m Model) dispatchTargets(action Action) (tea.Model, tea.Cmd) {
	switch {
	case m.repoint.confirm:
		return m.dispatchRepointConfirm(action)
	case m.repoint.open:
		return m.dispatchRepoint(action)
	}

	switch action {
	case ActionMoveUp:
		if m.targetsCur > 0 {
			m.targetsCur--
		}
	case ActionMoveDown:
		if m.targetsCur < len(m.cfg.Targets)-1 {
			m.targetsCur++
		}
	case ActionRepoint:
		return m.beginRepoint()
	case ActionCancel:
		m.screen = screenDashboard
	}
	return m, nil
}

// selectedTarget is the target the cursor points at. Reports false when there is
// nothing to select — a config with no targets is a state the screen already
// draws, and every verb here has to refuse rather than act on target 0.
func (m Model) selectedTarget() (store.Target, bool) {
	if m.targetsCur < 0 || m.targetsCur >= len(m.cfg.Targets) {
		return store.Target{}, false
	}
	return m.cfg.Targets[m.targetsCur], true
}

// beginRepoint opens the ref picker for the selected target and asks git what
// refs exist. Nothing is cached between openings: a ref can appear or vanish
// outside Drift, and a list of refs to *choose from* is only worth having if it
// is git's current answer.
func (m Model) beginRepoint() (tea.Model, tea.Cmd) {
	t, ok := m.selectedTarget()
	if !ok {
		return m, nil
	}
	m.repoint = repointState{open: true, key: t.Key, from: t.Ref, now: time.Now()}
	m.notice = ""
	return m, loadRemoteRefsCmd(m.repo)
}

// dispatchRepoint runs one action in the ref picker. Confirm does not write — it
// opens the confirmation, which is the one place the plan is stated.
func (m Model) dispatchRepoint(action Action) (tea.Model, tea.Cmd) {
	if m.repoint.filter.open {
		return m.repointFilterAction(action)
	}
	switch action {
	case ActionCancel:
		// esc backs out one step, and a filter left applied is a step: accepting a
		// query with enter closes the field but keeps the narrowing, so the next esc
		// undoes that rather than abandoning the picker. The wizard's rule, and it
		// exists there because a test found the version without it closed first-run
		// setup outright.
		if m.repoint.filter.active() {
			m.repoint = m.repoint.applyFilter(m.repoint.filter.clear())
			return m, nil
		}
		m.repoint = repointState{}
		return m, nil

	case ActionMoveUp:
		if m.repoint.cursor > 0 {
			m.repoint.cursor--
		}
		return m, nil

	case ActionMoveDown:
		if m.repoint.cursor < len(m.repoint.visible())-1 {
			m.repoint.cursor++
		}
		return m, nil

	case ActionFilter:
		var cmd tea.Cmd
		m.repoint.filter, cmd = m.repoint.filter.begin()
		return m, cmd

	case ActionConfirm:
		return m.pickRepointRef()
	}
	return m, nil
}

// repointFilterAction handles one of the filter keymap's control keys while the
// field has focus. Everything else types, routed through feedField — the split
// the wizard and the pairing checklist both use, and the only arrangement that
// lets `e`, `j` and `/` be part of a query on a screen that binds all three.
func (m Model) repointFilterAction(action Action) (tea.Model, tea.Cmd) {
	switch action {
	case ActionConfirm:
		m.repoint.filter = m.repoint.filter.commit() // keep the query, hand j/k back
		return m, nil
	case ActionCancel:
		m.repoint = m.repoint.applyFilter(m.repoint.filter.clear())
		return m, nil
	case ActionMoveUp:
		if m.repoint.cursor > 0 {
			m.repoint.cursor--
		}
		return m, nil
	case ActionMoveDown:
		if m.repoint.cursor < len(m.repoint.visible())-1 {
			m.repoint.cursor++
		}
		return m, nil
	}
	return m, nil
}

// pickRepointRef takes the highlighted ref to the confirmation. Picking the ref
// the target already names is a no-op said out loud rather than a write of
// nothing: it is the one outcome where "it worked" and "nothing happened" look
// identical on the row afterwards.
func (m Model) pickRepointRef() (tea.Model, tea.Cmd) {
	ref, ok := m.repoint.selectedRef()
	if !ok {
		return m, nil
	}
	if ref == m.repoint.from {
		m.notice = m.repoint.key + " already points at " + ref
		m.repoint = repointState{}
		return m, nil
	}
	m.repoint.to = ref
	m.repoint.confirm = true
	return m, nil
}

// dispatchRepointConfirm answers the y/n. Declining is the ordinary cancel and
// costs nothing: the picker is read-only up to this point, so there is nothing to
// undo — the same ordering `s` and `u` are built on.
func (m Model) dispatchRepointConfirm(action Action) (tea.Model, tea.Cmd) {
	switch action {
	case ActionCancel:
		m.repoint = repointState{}
		return m, nil
	case ActionConfirm:
		return m.commitRepoint()
	}
	return m, nil
}

// commitRepoint writes the re-pointed config.
//
// The new config is built here and handed to the Cmd, but **not** folded into the
// model: applyRepoint does that only once the write has succeeded. A model
// holding a ref config.json does not is worse here than anywhere else in the
// package, because the very next status sweep measures every branch against it —
// the numbers would be right about a target that does not exist on disk.
func (m Model) commitRepoint() (tea.Model, tea.Cmd) {
	r := m.repoint
	cfg, ok := m.cfg.SetTargetRef(r.key, r.to)
	if !ok {
		// The selection went stale — only reachable if the config changed under the
		// flow. Say so rather than write a target that is no longer there.
		m.notice = "no target keyed " + quote(r.key) + " any more — nothing was changed"
		m.repoint = repointState{}
		return m, nil
	}
	m.repoint = repointState{}
	m.notice = "pointing " + r.key + " at " + r.to + "…"
	return m, repointCmd(m.repo, cfg, r.key, r.from, r.to)
}

// applyRepoint folds a completed re-point in and re-reads every branch's
// standing against the ref that is now configured.
//
// The re-sweep is the rule areas 5 and 6 both landed on, and this is the case
// with the widest reach: a re-pointed target changes *every* paired branch's
// ↓behind at once, so a sweep left alone would report the old numbers against the
// new ref — the same class of lie as the declared badge before it re-read
// check-attr.
//
// It is a local sweep rather than a fetch, because the picker can only offer refs
// that already exist under refs/remotes: a ref you can pick is one you already
// have, so there is nothing here a fetch would make *resolvable*. How fresh it is
// remains the dashboard's `f`, which is a different question from which ref a
// target names — and folding a network round trip into a config correction would
// make it fail offline for no gain.
//
// No success notice, on purpose. The row behind this screen now shows the new
// ref, which is permanent where a notice is transient, and making the ref legible
// at rest is the whole reason 19e exists. A failure does get one — and nothing
// clears it, since a failed write starts no sweep.
func (m Model) applyRepoint(msg repointMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.notice = "couldn't re-point " + msg.key + ": " + msg.err.Error()
		return m, nil
	}

	m, id := m.supersedeSweeps()
	m.cfg = msg.cfg
	m.loading = true
	m.notice = ""
	return m, tea.Batch(m.spin.Tick,
		loadStatusCmd(context.Background(), m.repo, m.cfg, m.store.Tickets, id))
}

// applyRemoteRefs folds git's ref list into the open picker. A result that lands
// after the picker closed is dropped, the same guard every other async load here
// carries.
func (m Model) applyRemoteRefs(msg remoteRefsMsg) Model {
	if m.screen != screenTargets || !m.repoint.open {
		return m
	}
	m.repoint.loaded = true
	if msg.err != nil {
		m.notice = "couldn't list remote refs: " + msg.err.Error()
		return m
	}
	// Order preserved as given: git sorted by recency (RemoteBranches), and
	// re-sorting would throw away the one thing that answers "which of these is a
	// main" — the same reason the wizard leaves it alone.
	m.repoint.refs = msg.refs
	m.repoint.cursor = 0
	return m
}

// visible reports the indices of the refs surviving the filter, in list order.
// Derived on every call rather than stored (filter.go): the cursor means "the
// n-th visible row", so there is no second copy of the list to go stale against a
// query that has since changed.
func (r repointState) visible() []int {
	return filterVisible(len(r.refs), func(i int) bool { return r.filter.matches(r.refs[i].Ref) })
}

// selectedRef resolves the cursor to a ref. Reports false when the query matches
// nothing, which is the one state with no selected row at all.
func (r repointState) selectedRef() (string, bool) {
	vis := r.visible()
	if r.cursor < 0 || r.cursor >= len(vis) {
		return "", false
	}
	return r.refs[vis[r.cursor]].Ref, true
}

// applyFilter folds a changed query back in, keeping the cursor on the row it was
// on when that row survives the change (cursorFor). Every mutation of the query
// goes through here, so the cursor can never be left pointing past the end of a
// list that just shrank.
func (r repointState) applyFilter(f filterState) repointState {
	vis := r.visible()
	idx := -1
	if r.cursor >= 0 && r.cursor < len(vis) {
		idx = vis[r.cursor]
	}
	r.filter = f
	if idx < 0 {
		r.cursor = 0
		return r
	}
	r.cursor = cursorFor(r.visible(), idx)
	return r
}

// targetsView is the screen: the list in the shared frame, or whichever step of
// the re-point flow is drawn in its place.
func (m Model) targetsView() string {
	switch {
	case m.repoint.confirm:
		return m.screenView(m.repointConfirmBody(),
			helpLine(m.styles, m.width, nil, []string{"y re-point", "n cancel"}))
	case m.repoint.open && m.repoint.filter.open:
		return m.screenView(m.repointBody(), helpLine(m.styles, m.width,
			[]string{"type to filter", "↑/↓ move"}, []string{"enter accept", "esc clear"}))
	case m.repoint.open && m.repoint.filter.active():
		// esc means one thing at a time, and the line says which is live — so the
		// segment naming it is an anchor, never something a narrow terminal elides.
		return m.screenView(m.repointBody(), helpLine(m.styles, m.width,
			[]string{"j/k move", "/ refine"}, []string{"enter pick", "esc clear filter"}))
	case m.repoint.open:
		return m.screenView(m.repointBody(), helpLine(m.styles, m.width,
			[]string{"j/k move", "/ filter"}, []string{"enter pick", "esc back"}))
	}
	help := helpLine(m.styles, m.width,
		[]string{"j/k move", "e re-point"},
		[]string{"esc back", "? help", "q quit"})
	return m.screenView(m.targetsBody(), help)
}

// targetCols is the width budget for a target row: its two variable columns.
type targetCols struct{ key, ref int }

// targetRowFixed is what a target row costs before its variable columns: the
// 2-space indent and the 2-space separator between the key and the ref.
const targetRowFixed = 2 + 2

// targetColumns sizes the row against the panel it has to fit in, in the order
// of allocation every other row here uses (DESIGN.md §1): the cell carrying the
// row's *point* is costed so it can never be squeezed out, and the other absorbs
// what is left.
//
// Which cell that is inverts from the target picker's row, and deliberately. On
// the picker the key is what you are choosing and the ref is the disambiguator;
// here the ref *is* the subject — the whole screen exists because the ref was
// never on screen — so the key column takes its own bounded width first and the
// ref takes everything after it.
//
// A ref longer than what is left elides head-weighted (fit → elide), and what
// this comment used to warn against is now what it does — deliberately, and
// amended rather than overruled (roadmap area 20). The warning was: never show
// `origin/fix/…/mvp-3`, because `origin/fix/PSOT-22114-…` is the half that gives
// a wrong target away and the trailing `/mvp-3` is the half that made it look
// right. Re-read the string it objects to: it *does* show `origin/fix/`, which
// is the tell. The objection was to a middle-elide that split evenly or kept the
// first segment alone; head-weighted, the head 19e cares about survives and the
// suffix comes with it. A tail cut is the rule that loses a half outright.
func (m Model) targetColumns() targetCols {
	key := m.targetKeyWidth

	cw := rowWidth(m.styles, m.width)
	if cw <= 0 {
		// Size unknown: natural sizing, the same fallback branchColumns takes.
		return targetCols{key: key, ref: widestCell(len(m.cfg.Targets),
			func(i int) string { return m.cfg.Targets[i].Ref })}
	}

	avail := cw - targetRowFixed
	if key > avail-minRefCol {
		if key = avail - minRefCol; key < 0 {
			key = 0
		}
	}
	ref := avail - key
	if ref < 1 {
		ref = 1
	}
	return targetCols{key: key, ref: ref}
}

// targetsBody is the list, headed by what a target is, what `e` does to one, and
// where the rest of the file is edited.
//
// The path stays on screen now that `e` exists, because `e` covers exactly one
// field. A target's key, the unmergeable classes and the declare allow-list are
// still hand-edited, and the second line says which half is which rather than
// leaving a user to discover that `e` does not reach the key. It is left to wrap
// rather than clipped, since half a path is a path you cannot act on; headerLines
// costs a wrapping header line at the lines it really takes (window.go).
func (m Model) targetsBody() string {
	header := []string{
		m.styles.hint.Render("Targets — what your branches are compared against"),
		m.styles.help.Render("e points one at a different ref; keys and everything else are hand-edited:"),
		m.styles.help.Render(m.paths.Config),
		"",
	}

	if len(m.cfg.Targets) == 0 {
		return strings.Join(append(header,
			m.styles.hint.Render("No targets configured."),
			m.styles.help.Render("Drift compares nothing until this file names at least one."),
		), "\n")
	}

	cols := m.targetColumns()
	rows := make([]string, len(m.cfg.Targets))
	for i, t := range m.cfg.Targets {
		// The ref carries the row, so it takes the ordinary foreground and the key
		// takes the target style — the reverse weighting of the picker row, where
		// the ref is the small print beside the thing being chosen.
		rows[i] = fmt.Sprintf("  %s  %s",
			m.styles.target.Render(fit(t.Key, cols.key)),
			m.styles.branch.Render(fit(t.Ref, cols.ref)))
	}
	return listBody(m.styles, m.width, m.height, header, rows, m.targetsCur)
}

// currentRefLabel marks the ref the target already points at. The picker is a
// list of refs with nothing to distinguish one from another, so without it the
// user has no way to tell what they are changing *from* — and picking the
// ref already configured would look like a change that did nothing.
const currentRefLabel = "current"

// repointRowFixed is what a picker row costs before its ref column: the age
// column, the space after it, and the two before the label.
const repointRowFixed = ageColWidth + 1 + 2

// repointBody is the ref picker: every remote-tracking ref, newest first, with
// the one the target already names marked.
//
// The same offer and the same order as the first-run wizard, deliberately —
// picking a target's ref is the wizard's own question asked again about one
// target, so it is answered from the same list, with the same age column making
// the sort order legible, and with `/` for the same reason (area 14: this is the
// one list where filtering is load-bearing, because it is every ref in the repo).
func (m Model) repointBody() string {
	r := m.repoint
	header := []string{
		m.styles.hint.Render("Point " + r.key + " at a different ref"),
		// Sized to fit the near-universal 80 columns without wrapping. headerLines
		// costs a wrapped line honestly, so a longer sentence would be *correct* —
		// it would just spend a row of the list to say the same thing (area 15).
		m.styles.help.Render("Branches paired to it compare against your pick. Newest first."),
		"",
	}

	if !r.loaded {
		return strings.Join(append(header, m.styles.help.Render("reading the repo's refs…")), "\n")
	}
	if len(r.refs) == 0 {
		return strings.Join(append(header,
			m.styles.hint.Render("No remote-tracking refs to offer."),
			m.styles.help.Render("Nothing under refs/remotes — fetch the remote, then try again."),
		), "\n")
	}

	vis := r.visible()
	if r.filter.open || r.filter.active() {
		// No hidden-selected count, because there is nothing to hide: this is a
		// single pick made and acted on in one step, not a checklist carrying state
		// the filter could disagree with (filter.go).
		header = append(header, r.filter.line(m.styles, len(vis), len(r.refs), 0), "")
	}
	if len(vis) == 0 {
		return strings.Join(append(header,
			m.styles.help.Render("No ref matches "+quote(r.filter.query())+" — esc clears the filter.")), "\n")
	}

	// The label is the row's fixed cost and is paid first; the ref absorbs what is
	// left and ellipsises at its tail, the same allocation and the same end to lose
	// as the targets list itself (targetColumns).
	labels := make([]string, len(vis))
	for i, ri := range vis {
		if r.refs[ri].Ref == r.from {
			labels[i] = m.styles.help.Render(currentRefLabel)
		}
	}
	labelWidth := widestCell(len(labels), func(i int) string { return labels[i] })

	refWidth := widestCell(len(vis), func(i int) string { return r.refs[vis[i]].Ref })
	if cw := rowWidth(m.styles, m.width); cw > 0 {
		if avail := cw - repointRowFixed - labelWidth; refWidth > avail {
			if refWidth = avail; refWidth < minRefCol {
				refWidth = minRefCol
			}
		}
	}

	rows := make([]string, len(vis))
	for i, ri := range vis {
		b := r.refs[ri]
		rows[i] = fmt.Sprintf("%s %s  %s",
			m.styles.help.Render(padLeft(relativeAge(b.Updated, r.now), ageColWidth)),
			m.styles.branch.Render(fit(b.Ref, refWidth)),
			labels[i])
	}
	return listBody(m.styles, m.width, m.height, header, rows, r.cursor)
}

// repointConfirmBody is the y/n between a picked ref and config.json being
// rewritten.
//
// It names both refs, and the *from* is as load-bearing as the *to*: the whole
// finding behind 19e is that a wrong ref reads as correct through its key, so the
// value being replaced is the one the user has never seen. Both are bounded by
// boundRef, which elides **head-weighted**: `origin/fix/PSOT-22114-…` is what
// gives a wrong target away, so the head is what the rule protects. It reads as
// a reversal of 19a and is an amendment to it — 19a's objection was to a
// middle-elide that would drop the head, and area 20's keeps it while adding the
// suffix back. What must never happen here is the head going, whichever
// mechanism takes it.
//
// The prose is name-free and short enough to survive the 60-column floor without
// clipping. Interpolating the key into it would put an unbounded value in the one
// place the meaning has to land whole — 17b's overlay learned that by measuring
// itself at 80 columns and finding the guarantee cut mid-word.
func (m Model) repointConfirmBody() string {
	r := m.repoint
	lines := []string{
		m.styles.hint.Render("Re-point " + r.key),
		"",
		m.styles.help.Render("  from  " + m.boundRef("  from  ", r.from)),
		m.styles.branch.Render("  to    " + m.boundRef("  to    ", r.to)),
		"",
		m.styles.help.Render("  Every paired branch is measured against it."),
		m.styles.help.Render("  Only this ref changes in config.json."),
		"",
		m.styles.hint.Render("  Re-point it?  (y/n)"),
	}
	for i, l := range lines {
		lines[i] = clipPanelLine(m.styles, m.width, l)
	}
	return strings.Join(lines, "\n")
}
