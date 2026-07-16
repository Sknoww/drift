# Drift — Design System

> Living design reference for Drift. The **single source of truth** for visual and
> interaction decisions. Update it as decisions are made — every new pattern,
> spacing rule, or component convention goes here before it goes in code.

The visual tone is **dense, technical, keyboard-driven**. The quality bar is
**lazygit**: polished bordered panels, color-coded state, everything reachable from
the home row. "Looks like raw text output" is a bug.

**Status legend:** ✅ Decided · 🚧 In progress · ❓ Open question · 📝 Placeholder (TBD)

---

## 1. Foundations 📝

- **Color roles** — meaning first, palette later. `behind > 0` is the one alarm that
  matters (the target main moved under me) and reads as a warning color; `ahead` is
  neutral. Dirty is a dot, not a color shift. The checked-out branch gets a marker.
  The selected row is a highlighted band. 📝 Pin actual Lip Gloss colors on first
  build.
- **Panels** — Lip Gloss bordered and titled. 📝 Border style + title placement TBD.
- **Density** — a ticket is one row; expanding it lists its 2–3 branches one per row
  beneath. The whole point is seeing a ticket's fan-out without scrolling.
- **Status cluster** — per branch: target label · `↓behind ↑ahead` · dirty dot ·
  checked-out marker. Fixed order, aligned into columns so the eye scans down.

**The Model holds:** loaded `Config` + `Store`; current screen; cursor position; which
ticket is expanded; a computed status map keyed by `ticketID + branch`; a
`textinput.Model`; add-flow state; window size; last error.

**Screens:** Dashboard (tickets, selected one expanded to its branches) · Add ticket
(ID entry) · Add ticket (pairing checklist).

## 2. Components 📝

- **Ticket row** — ID, optional title, collapsed/expanded affordance.
- **Branch row** — branch name + the status cluster above.
- **Candidate checklist** (add flow) — space toggles; a number/cycle key assigns the
  target per selected branch. Must make "which target does this map to?" unmissable —
  an unassigned selection is an error state, since the software never guesses.
- **Diff panel** (area 4) — the `.uwe` incoming diff, styled, read-only. This panel
  is the replacement for opening GitLab; it earns real polish.

## 3. Motion & interaction 📝

**The keymap is a contract** — these bindings are a decision, not an implementation
detail. Dashboard:

| Key | Action |
|---|---|
| `j` / `k` / arrows | Move |
| `enter` / `space` | Expand / collapse ticket |
| `a` | Add ticket |
| `d` | Delete selected ticket |
| `r` | Refresh statuses |
| `f` | Fetch, then refresh |
| `q` / `ctrl+c` | Quit |

Add flow: `space` toggles a candidate, `enter` saves, `esc` cancels.

Git work runs as async `Cmd`s, so every one of these stays responsive — the UI must
never freeze on a fetch. 📝 Loading/empty/error states TBD: at minimum an empty
dashboard needs to teach "press `a`", and a failed Git call needs to say so without
tearing down the view.
