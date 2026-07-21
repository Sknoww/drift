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
- **Density** — a ticket is one row; expanding it lists its branches one per row
  beneath, one per target it aims at. Seeing a ticket's whole fan-out without
  scrolling is the point — but the target count is config-driven and unbounded, so
  "fits on screen" is a goal, not an invariant. An expanded ticket whose fan-out
  overflows must scroll gracefully rather than break the layout.
- **Status cluster** — per branch: target label · `↓behind ↑ahead` · dirty dot ·
  checked-out marker. Fixed order, aligned into columns so the eye scans down. The
  target label is **variable width** — column widths are computed from the config's
  longest `Target.Key`, never hardcoded.

**The Model holds:** loaded `Config` + `Store`; current screen; cursor position; which
ticket is expanded; a computed status map keyed by `ticketID + branch`; a
`textinput.Model`; add-flow state; window size; last error.

**Screens:** Dashboard (tickets, selected one expanded to its branches) · Add ticket
(ID entry) · Add ticket (pairing checklist, with the target picker as an overlay on
top of it).

## 2. Components 📝

- **Ticket row** — ID, optional title, collapsed/expanded affordance.
- **Branch row** — branch name + the status cluster above.
- **Candidate checklist** (add flow) — space toggles a branch. Must make "which target
  does this map to?" unmissable — an unassigned selection is an error state, since the
  software never guesses.
- **Target picker overlay** ✅ — the general mechanism for assigning a target to a
  selected candidate branch. The original number/cycle-key idea assumed three targets
  and does not survive an unbounded config: number keys run out at 9, and cycling
  through 12 targets to reach the last one is miserable. The picker lists every
  configured target in config order, showing `Key` and `Ref` so the choice is
  unambiguous when keys are terse. Number keys stay as an accelerator for the first 9
  (see §3) — the picker is the mechanism, numbers are the shortcut. Because targets are
  unbounded, the overlay scrolls and never assumes its list fits.
- **Diff panel** (area 5) — the incoming diff for an unmergeable file, styled,
  read-only. This panel is the replacement for opening the web UI to hunt for changes;
  it earns real polish. **Plain text for every unmergeable format, always** —
  format-specific rendering is a different product and explicitly out of scope.

## 3. Motion & interaction 📝

**Named actions are the contract; keys are a rebindable default.** The *actions* below
(move, expand, add, fetch, `local_only`…) are the stable interface every screen
dispatches on — never a raw key literal, so customization is a pure override layer and
not a retrofit. The default **keys** are considered, not sacred: a user-global keymap
(area 12) can rebind any action, and any action left unbound keeps the default here.
Dashboard:

| Key | Action |
|---|---|
| `j` / `k` / arrows | Move |
| `enter` / `space` | Expand / collapse ticket |
| `a` | Add ticket |
| `d` | Delete selected ticket |
| `r` | Refresh statuses |
| `f` | Fetch, then refresh |
| `l` | Manage local-only changes (area 6) |
| `q` / `ctrl+c` | Quit |

Add flow (pairing checklist):

| Key | Action |
|---|---|
| `j` / `k` / arrows | Move |
| `space` | Toggle candidate branch |
| `t` | Open the target picker for the selected candidate |
| `1`–`9` | Accelerator: assign the Nth configured target directly, no picker |
| `enter` | Save |
| `esc` | Cancel |

Target picker overlay: `j` / `k` move, `enter` selects, `esc` cancels — deliberately
the same shape as the dashboard, so the overlay needs no learning. Targets past the
9th are reachable only through the picker, which is exactly why it's the mechanism and
the number keys are only a shortcut.

Git work runs as async `Cmd`s, so every one of these stays responsive — the UI must
never freeze on a fetch. 📝 Loading/empty/error states TBD: at minimum an empty
dashboard needs to teach "press `a`", and a failed Git call needs to say so without
tearing down the view.
