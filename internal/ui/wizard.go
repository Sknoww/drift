package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

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
	styles styles
	keys   Keymap

	targets []wizardTarget
	cursor  int

	editing bool            // an inline key rename is open over the checklist
	input   textinput.Model // the key editor, live only while editing

	width, height int

	notice   string
	declined bool           // esc/ctrl+c: fall back to the hand-edit path
	done     bool           // enter with a valid selection: result is set
	result   []store.Target // the confirmed targets, valid only when done
}

// wizardTarget is one remote ref offered as a target. key seeds Target.Key and
// is editable; ref is the picked remote ref and becomes Target.Ref verbatim, so
// a target can never compare against a typo.
type wizardTarget struct {
	ref      string
	key      string
	included bool
}

// RunWizard runs the first-run wizard over the given remote refs and reports the
// targets the user chose. ok is false when the user declined (esc/ctrl+c) or
// selected nothing, in which case the caller falls back to the placeholder
// config. remoteRefs must be non-empty — an empty offer has nothing to pick, so
// the caller gates on it and falls back before ever reaching here.
func RunWizard(repo *git.Repo, remoteRefs []string) (targets []store.Target, ok bool, err error) {
	_ = repo // the wizard does no git of its own; refs are gathered by the caller
	out, err := tea.NewProgram(newWizard(remoteRefs), tea.WithAltScreen()).Run()
	if err != nil {
		return nil, false, err
	}
	m := out.(wizardModel)
	if !m.done || m.declined {
		return nil, false, nil
	}
	return m.result, true, nil
}

func newWizard(remoteRefs []string) wizardModel {
	targets := make([]wizardTarget, len(remoteRefs))
	for i, ref := range remoteRefs {
		targets[i] = wizardTarget{ref: ref, key: deriveKey(ref)}
	}
	return wizardModel{
		styles:  newStyles(),
		keys:    DefaultWizardKeys(),
		targets: targets,
	}
}

// deriveKey seeds a target's key from its ref by dropping the remote prefix:
// origin/main -> main, origin/feature/x -> feature/x. It is only a default; the
// user renames it with e.
func deriveKey(ref string) string {
	if _, rest, found := strings.Cut(ref, "/"); found {
		return rest
	}
	return ref
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
	if action, ok := m.keys.action(msg.String()); ok {
		return m.dispatch(action)
	}
	return m, nil
}

func (m wizardModel) dispatch(action Action) (tea.Model, tea.Cmd) {
	switch action {
	case ActionQuit, ActionCancel:
		m.declined = true
		return m, tea.Quit

	case ActionMoveUp:
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case ActionMoveDown:
		if m.cursor < len(m.targets)-1 {
			m.cursor++
		}
		return m, nil

	case ActionToggleCandidate:
		if len(m.targets) > 0 {
			m.targets[m.cursor].included = !m.targets[m.cursor].included
			m.notice = ""
		}
		return m, nil

	case ActionEditKey:
		return m.beginEdit()

	case ActionConfirm:
		return m.save()
	}
	return m, nil
}

// beginEdit opens the inline key editor for the selected ref. Renaming a ref
// includes it — you do not rename a target you are not keeping — mirroring the
// pairing flow, where assigning a target includes the candidate.
func (m wizardModel) beginEdit() (tea.Model, tea.Cmd) {
	if len(m.targets) == 0 {
		return m, nil
	}
	ti := textinput.New()
	ti.CharLimit = 64
	ti.SetValue(m.targets[m.cursor].key)
	ti.CursorEnd()
	ti.Focus()

	m.input = ti
	m.editing = true
	m.targets[m.cursor].included = true
	m.notice = ""
	return m, textinput.Blink
}

// commitEdit applies the edited key. An empty key keeps the editor open with a
// hint rather than saving a nameless target.
func (m wizardModel) commitEdit() wizardModel {
	key := strings.TrimSpace(m.input.Value())
	if key == "" {
		m.notice = "a target key can't be empty"
		return m
	}
	m.targets[m.cursor].key = key
	m.editing = false
	m.notice = ""
	return m
}

// save builds the targets from the included refs and hands them back. It blocks
// on the same conditions store.SaveConfig would reject, so the user fixes them
// here with a clear pointer rather than seeing a write error: an empty key, a
// duplicate key, or nothing selected. Any number of targets is valid, including
// one — the wizard no more implies a count than the placeholder does.
func (m wizardModel) save() (tea.Model, tea.Cmd) {
	var targets []store.Target
	seen := make(map[string]bool)
	for _, t := range m.targets {
		if !t.included {
			continue
		}
		key := strings.TrimSpace(t.key)
		if key == "" {
			m.notice = "give " + t.ref + " a key (e) before saving"
			return m, nil
		}
		if seen[key] {
			m.notice = "duplicate key " + quote(key) + " — rename one (e) before saving"
			return m, nil
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
		m.styles.hint.Render("Pick the target branches Drift tracks against."),
		m.styles.help.Render("Your long-lived mains — one per version of the code in flight. Any number."),
		"",
	}

	keyWidth := m.keyColWidth()
	rows := make([]string, len(m.targets))
	for i, t := range m.targets {
		box := "[ ]"
		if t.included {
			box = "[x]"
		}

		keyCell := padRight(t.key, keyWidth)
		if m.editing && i == m.cursor {
			keyCell = m.input.View()
		}

		rows[i] = fmt.Sprintf("%s %s  %s %s",
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
	return m.styles.help.Render("j/k move · space select · e rename key · enter save · esc skip (hand-edit)")
}

// keyColWidth aligns the key column at the widest key, so the ← arrows line up.
func (m wizardModel) keyColWidth() int {
	w := 0
	for _, t := range m.targets {
		if len(t.key) > w {
			w = len(t.key)
		}
	}
	return w
}
