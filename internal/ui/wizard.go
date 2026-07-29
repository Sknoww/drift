package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Sknoww/drift/internal/git"
	"github.com/Sknoww/drift/internal/store"
)

// wizardModel is the first-run setup wizard (roadmap area 4). It runs as its own
// Bubble Tea program before the dashboard, because the dashboard Model is built
// over an already-valid Config and on first run there is none yet.
//
// It offers the repo's own remote refs as targets — never local branches, since
// a target is compared against its origin/<name> ref (CONTEXT.md, area 4) — and
// follows the pairing checklist's rule: show real refs, let the user choose,
// never guess. It writes nothing itself; on confirm it returns the chosen
// targets and RunWizard's caller persists them.
type wizardModel struct {
	styles     styles
	keys       Keymap
	filterKeys Keymap // the control keys live only while the filter field has focus

	targets []wizardTarget
	cursor  int         // index into visible(), not into targets
	filter  filterState // type-to-filter over the offered refs (area 14)

	editing bool            // an inline key rename is open over the checklist
	input   textinput.Model // the key editor, live only while editing

	// now is the clock the age column is rendered against, captured once at
	// construction. The wizard is a screen you are on for seconds, so an age
	// that ticks would be motion with nothing to say — and a fixed clock keeps
	// the column assertable in a test.
	now time.Time

	width, height int

	notice   string
	declined bool           // esc/ctrl+c: fall back to the hand-edit path
	done     bool           // enter with a valid selection: result is set
	result   []store.Target // the confirmed targets, valid only when done
}

// wizardTarget is one remote ref offered as a target. key seeds Target.Key and
// is editable; ref is the picked remote ref and becomes Target.Ref verbatim, so
// a target can never compare against a typo. updated is the ref's tip date,
// carried only to render the age column — it is never written to the config.
type wizardTarget struct {
	ref      string
	key      string
	updated  time.Time
	included bool
}

// RunWizard runs the first-run wizard over the given remote branches and reports
// the targets the user chose. ok is false when the user declined (esc/ctrl+c) or
// selected nothing, in which case the caller falls back to the placeholder
// config. branches must be non-empty — an empty offer has nothing to pick, so
// the caller gates on it and falls back before ever reaching here. Their order
// is preserved as given: git sorted them by recency (git.RemoteBranches), and
// re-sorting here would throw that away.
func RunWizard(repo *git.Repo, branches []git.RemoteBranch) (targets []store.Target, ok bool, err error) {
	_ = repo // the wizard does no git of its own; refs are gathered by the caller
	out, err := tea.NewProgram(newWizard(branches), tea.WithAltScreen()).Run()
	if err != nil {
		return nil, false, err
	}
	m := out.(wizardModel)
	if !m.done || m.declined {
		return nil, false, nil
	}
	return m.result, true, nil
}

func newWizard(branches []git.RemoteBranch) wizardModel {
	targets := make([]wizardTarget, len(branches))
	for i, b := range branches {
		targets[i] = wizardTarget{ref: b.Ref, key: deriveKey(b.Ref), updated: b.Updated}
	}
	return wizardModel{
		styles:     newStyles(),
		keys:       DefaultWizardKeys(),
		filterKeys: DefaultFilterKeys(),
		targets:    targets,
		now:        time.Now(),
	}
}

// visible reports the indices of the targets surviving the filter, in list
// order. Derived on every call rather than stored (filter.go): there is no
// second copy of the list to keep in sync, and the cursor's meaning — "the n-th
// visible row" — can never go stale against a query that has since changed.
//
// Both halves of the row are matched, because the row shows both: a user reading
// `main ← origin/main` should be able to type either one.
func (m wizardModel) visible() []int {
	return filterVisible(len(m.targets), func(i int) bool {
		return m.filter.matches(m.targets[i].key, m.targets[i].ref)
	})
}

// selected resolves the cursor to an index into targets. Reports false when the
// query matches nothing, which is the one state where there is no selected row
// at all — every verb on this screen has to refuse rather than act on target 0.
func (m wizardModel) selected() (int, bool) {
	vis := m.visible()
	if m.cursor < 0 || m.cursor >= len(vis) {
		return 0, false
	}
	return vis[m.cursor], true
}

// applyFilter folds a changed query back in, keeping the cursor on the row it
// was on when that row survives the change (cursorFor). Every mutation of the
// query goes through here, so the cursor can never be left pointing past the end
// of a list that just shrank.
func (m wizardModel) applyFilter(f filterState) wizardModel {
	idx, ok := m.selected()
	m.filter = f
	if !ok {
		m.cursor = 0
		return m
	}
	m.cursor = cursorFor(m.visible(), idx)
	return m
}

// reveal drops the filter and puts the cursor on targets[idx].
//
// A save blocked by a ref the query is currently hiding would otherwise name a
// row the user cannot see — the exact failure the counts exist to prevent. The
// screen fixes it rather than reporting it: revealing a row is not a choice made
// on the user's behalf, it is showing them the one they already made.
func (m wizardModel) reveal(idx int) wizardModel {
	m.filter = m.filter.clear()
	m.cursor = cursorFor(m.visible(), idx)
	return m
}

// keySeedWidth is how long a derived key may get before deriveKey stops keeping
// the whole path. Sized so the multi-segment refs that carry real meaning —
// release/2.0, hotfix/2.0 — survive intact, while the ones nobody would type do
// not.
const keySeedWidth = 16

// deriveKey seeds a target's key from its ref by dropping the remote prefix,
// keeping the rest of the path while it stays terse and falling back to the
// ref's last segment when it does not:
//
//	origin/main                        -> main
//	origin/release/2.0                 -> release/2.0
//	origin/feature/TEAM-1234-long-name -> TEAM-1234-long-name
//
// A key is a terse UI label — main, r2perf — and the whole path after the remote
// is not one: nobody would type feature/TEAM-1234-long-name to name a target,
// and every dashboard row would carry it. But cutting to the last segment
// unconditionally throws away the half that says what a ref *is*, turning
// release/2.0 and hotfix/2.0 into two targets both called 2.0. The threshold
// keeps the short ones whole and only shortens what was the actual complaint.
//
// It is a seed either way, never a decision: the wizard shows the key beside the
// ref it came from, e renames it, and a duplicate blocks the save with the row
// revealed. Nothing here is guessed on the user's behalf and left unseen.
func deriveKey(ref string) string {
	_, rest, found := strings.Cut(ref, "/")
	if !found {
		return ref
	}
	if lipgloss.Width(rest) <= keySeedWidth {
		return rest
	}
	if i := strings.LastIndex(rest, "/"); i >= 0 {
		return rest[i+1:]
	}
	return rest // one long segment: there is nothing shorter to take, so e it is
}

func (m wizardModel) Init() tea.Cmd { return nil }

func (m wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	if m.editing {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	if m.filter.open {
		// Cursor blink and friends; a non-key message cannot change the query, so
		// the visible set needs no re-derivation here.
		var cmd tea.Cmd
		m.filter, cmd = m.filter.typed(msg)
		return m, cmd
	}
	return m, nil
}

// handleKey resolves a key through the wizard keymap. While a rename is open the
// control keys are handled here and every other key types into the field, the
// same split the ID-entry screen uses.
func (m wizardModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.editing {
		switch msg.String() {
		case "enter":
			return m.commitEdit(), nil
		case "esc":
			m.editing = false // cancel the rename, keep the previous key
			return m, nil
		case "ctrl+c":
			m.declined = true
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	if m.filter.open {
		return m.filterKey(msg)
	}
	if action, ok := m.keys.action(msg.String()); ok {
		return m.dispatch(action)
	}
	return m, nil
}

// filterKey handles a keystroke while the filter field has focus. Only the
// control keys of the filter keymap act; everything else types, which is the
// same split the rename editor above and the ID-entry screen use — and the only
// arrangement that lets `e`, `j` and `space` be part of a query on a screen that
// binds all three.
func (m wizardModel) filterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if action, ok := m.filterKeys.action(msg.String()); ok {
		switch action {
		case ActionQuit:
			m.declined = true
			return m, tea.Quit
		case ActionConfirm:
			m.filter = m.filter.commit() // keep the query, hand j/k back to the list
			return m, nil
		case ActionCancel:
			return m.applyFilter(m.filter.clear()), nil
		case ActionMoveUp, ActionMoveDown:
			return m.dispatch(action)
		}
	}
	f, cmd := m.filter.typed(msg)
	return m.applyFilter(f), cmd
}

func (m wizardModel) dispatch(action Action) (tea.Model, tea.Cmd) {
	switch action {
	case ActionQuit:
		m.declined = true
		return m, tea.Quit

	case ActionCancel:
		// esc backs out one step, and a filter left applied *is* a step: accepting
		// a query with enter closes the field but keeps the narrowing, so the next
		// esc has to undo that rather than abandon the wizard. Declining first-run
		// setup by accident, because the last thing you did was narrow a list, is
		// exactly the kind of surprise the one-step-at-a-time rule exists to stop.
		if m.filter.active() {
			return m.applyFilter(m.filter.clear()), nil
		}
		m.declined = true
		return m, tea.Quit

	case ActionMoveUp:
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case ActionMoveDown:
		if m.cursor < len(m.visible())-1 {
			m.cursor++
		}
		return m, nil

	case ActionToggleCandidate:
		if idx, ok := m.selected(); ok {
			m.targets[idx].included = !m.targets[idx].included
			m.notice = ""
		}
		return m, nil

	case ActionEditKey:
		return m.beginEdit()

	case ActionFilter:
		var cmd tea.Cmd
		m.filter, cmd = m.filter.begin()
		return m, cmd

	case ActionConfirm:
		return m.save()
	}
	return m, nil
}

// beginEdit opens the inline key editor for the selected ref. Renaming a ref
// includes it — you do not rename a target you are not keeping — mirroring the
// pairing flow, where assigning a target includes the candidate.
func (m wizardModel) beginEdit() (tea.Model, tea.Cmd) {
	idx, ok := m.selected()
	if !ok {
		return m, nil
	}
	ti := textinput.New()
	ti.CharLimit = 64
	ti.SetValue(m.targets[idx].key)
	ti.CursorEnd()
	ti.Focus()

	m.input = ti
	m.editing = true
	m.targets[idx].included = true
	m.notice = ""
	return m, textinput.Blink
}

// commitEdit applies the edited key. An empty key keeps the editor open with a
// hint rather than saving a nameless target.
//
// A rename can move the row out of the current filter — keys are matched, so
// renaming `main` to `r2perf` under the query "main" hides what you just typed.
// That is correct rather than a bug (the query still means what it says), and it
// is why the edit re-derives the cursor: the row is now selected-but-hidden, and
// the filter line's count is what says so.
func (m wizardModel) commitEdit() wizardModel {
	idx, ok := m.selected()
	if !ok {
		m.editing = false
		return m
	}
	key := strings.TrimSpace(m.input.Value())
	if key == "" {
		m.notice = "a target key can't be empty"
		return m
	}
	m.targets[idx].key = key
	m.editing = false
	m.notice = ""
	m.cursor = cursorFor(m.visible(), idx)
	return m
}

// save builds the targets from the included refs and hands them back. It blocks
// on the same conditions store.SaveConfig would reject, so the user fixes them
// here with a clear pointer rather than seeing a write error: an empty key, a
// duplicate key, or nothing selected. Any number of targets is valid, including
// one — the wizard no more implies a count than the placeholder does.
//
// It iterates every target, never just the visible ones: filtering is a render
// concern (DESIGN.md §1), so a ref selected and then filtered out is still
// saved. The corollary is that a block can land on a ref the query is hiding, so
// each one reveals the row it names before reporting it.
func (m wizardModel) save() (tea.Model, tea.Cmd) {
	var targets []store.Target
	seen := make(map[string]bool)
	for i, t := range m.targets {
		if !t.included {
			continue
		}
		key := strings.TrimSpace(t.key)
		if key == "" {
			m.notice = "give " + t.ref + " a key (e) before saving"
			return m.reveal(i), nil
		}
		if seen[key] {
			m.notice = "duplicate key " + quote(key) + " — rename one (e) before saving"
			return m.reveal(i), nil
		}
		seen[key] = true
		targets = append(targets, store.Target{Key: key, Ref: t.ref})
	}
	if len(targets) == 0 {
		m.notice = "select at least one target (space) — or esc to edit the config by hand"
		return m, nil
	}

	m.result = targets
	m.done = true
	return m, tea.Quit
}

func (m wizardModel) View() string {
	if v, ok := tooNarrowView(m.styles, m.width); ok {
		return m.styles.app.Render(v)
	}
	var b strings.Builder
	b.WriteString(m.styles.title.Render("drift") + "  " + m.styles.help.Render("first-run setup"))
	b.WriteString("\n")

	b.WriteString(panelStyle(m.styles, m.width).Render(m.body()))
	b.WriteString("\n")

	if m.notice != "" {
		b.WriteString(m.styles.hint.Render(m.notice))
		b.WriteString("\n")
	}
	b.WriteString(m.help())
	return m.styles.app.Render(b.String())
}

// body is the checklist of remote refs: a box, the (editable) key, and the ref
// it maps to, so a terse key is never ambiguous — the same Key←Ref shape as the
// area-3 target picker.
func (m wizardModel) body() string {
	header := []string{
		m.styles.hint.Render("Pick the target branches Drift tracks against. Newest first."),
		m.styles.help.Render("Your long-lived mains — one per version of the code in flight. Any number."),
		"",
	}

	vis := m.visible()
	if m.filter.open || m.filter.active() {
		hidden := hiddenSelectedCount(len(m.targets), vis, func(i int) bool { return m.targets[i].included })
		header = append(header, m.filter.line(m.styles, len(vis), len(m.targets), hidden), "")
	}
	if len(vis) == 0 {
		return strings.Join(append(header,
			m.styles.help.Render("No ref matches "+quote(m.filter.query())+" — esc clears the filter.")), "\n")
	}

	keyWidth := m.keyColWidth(vis)
	rows := make([]string, len(vis))
	for i, ti := range vis {
		t := m.targets[ti]
		box := "[ ]"
		if t.included {
			box = "[x]"
		}

		keyCell := fit(t.key, keyWidth)
		if m.editing && i == m.cursor {
			// The live field is never fitted: it owns its own width, and clipping
			// what the user is typing would hide the tail of their own edit.
			keyCell = m.input.View()
		}

		// The age leads the row rather than trailing it. It is the one column
		// here with a fixed width, so at the left edge it always aligns and
		// clipRow can never eat it — and read top to bottom it *is* the sort
		// order, which is what keeps that order from looking arbitrary.
		rows[i] = fmt.Sprintf("%s %s %s  %s %s",
			m.styles.help.Render(padLeft(relativeAge(t.updated, m.now), ageColWidth)),
			box,
			m.styles.target.Render(keyCell),
			m.styles.help.Render("←"),
			m.styles.branch.Render(t.ref))
	}
	return listBody(m.styles, m.width, m.height, header, rows, m.cursor)
}

func (m wizardModel) help() string {
	if m.editing {
		return m.styles.help.Render("enter save name · esc cancel edit")
	}
	if m.filter.open {
		return m.styles.help.Render("type to filter · ↑/↓ move · enter accept · esc clear")
	}
	// esc means one thing at a time, and the line says which is live — with a
	// filter applied it undoes the filter, not the wizard.
	if m.filter.active() {
		return m.styles.help.Render("j/k move · / refine · space select · e rename key · enter save · esc clear filter")
	}
	return m.styles.help.Render("j/k move · / filter · space select · e rename key · enter save · esc skip")
}

// ageColWidth is the fixed width of the age column, sized to the longest value
// relativeAge can produce ("12mo"). Fixed rather than measured: a column that
// grew with its content would be one more thing pushing the ref name off a
// panel, which is the failure area 14 spent itself fixing.
const ageColWidth = 4

// wizardRowFixed is what a wizard row costs before its key column: the age
// column, the box, and the three separators around key and ← .
const wizardRowFixed = ageColWidth + 1 + 3 + 1 + 1 + 1 + 1

// relativeAge renders how long ago a ref's tip was committed, in the narrowest
// form that still says it: now, 5m, 3h, 2d, 7mo, 2y.
//
// It exists to make the sort order legible. An unexplained ordering reads as
// arbitrary, or as a bug; a column of ages descending down the left edge says
// what the list is doing without spending a line of prose to explain it. And it
// answers the wizard's own question directly — a ref touched two days ago reads
// as a main, one untouched for fourteen months does not.
//
// A zero time means git reported no usable date (RemoteBranches). That renders
// empty rather than as an age: guessing one would be the column lying about the
// single thing it is here to say.
func relativeAge(tip, now time.Time) string {
	if tip.IsZero() {
		return ""
	}
	// A tip dated in the future — clock skew, or a rewritten date — falls out as
	// "now" rather than as a negative age.
	switch d := now.Sub(tip); {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		days := int(d.Hours() / 24)
		switch {
		case days < 30:
			return fmt.Sprintf("%dd", days)
		case days < 365:
			return fmt.Sprintf("%dmo", days/30)
		default:
			return fmt.Sprintf("%dy", days/365)
		}
	}
}

// keyColWidth aligns the key column at the widest key, so the ← arrows line up —
// bounded by its cap and by what the panel has left for the ref, since a key is
// user-supplied (e renames it to anything) and one long one used to pad every
// drawn row past the panel edge. That padding is what doubled the frame in area
// 14's measurements; deriveKey stopped seeding the long ones, and this stops a
// hand-typed one doing the same.
//
// Measured over the visible rows only: the column exists to align what is on
// screen, and widening it for a ref the filter is hiding would pad every drawn
// row past a panel none of them need (DESIGN.md §1).
func (m wizardModel) keyColWidth(visible []int) int {
	w := widestCell(len(visible), maxKeyCol, func(i int) string { return m.targets[visible[i]].key })
	cw := contentWidth(m.styles, m.width)
	if cw <= 0 {
		return w // size unknown: natural sizing
	}
	// Leave the ref — the load-bearing half of the row — a usable share.
	if avail := cw - wizardRowFixed - minNameCol; w > avail {
		if w = avail; w < 1 {
			w = 1
		}
	}
	return w
}
