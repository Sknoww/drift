# ADR 0001 — The selected row is a band *and* a left-edge marker

**Status:** Accepted · 2026-07-28 · supersedes the band-only rule in `DESIGN.md` §1/§3

## Context

`DESIGN.md` §1 pinned the selected row as "a highlighted band", and §3 pinned it as
a background filling the panel's full inner width. Roadmap area 15 measured what
that band actually renders as: ANSI 236 = `rgb(48,48,48)`, giving a contrast ratio
against common terminal backgrounds of **1.06:1** on One Dark, 1.08 Dracula, 1.12
Gruvbox, 1.26 VS Code Dark, and 1.59 on pure black — its best case. On most modern
themes the band sits within ~3% luminance of the page. The selected row was not
badly designed so much as **almost not drawn**.

The roadmap recorded two candidate directions and was explicit that argument could
not settle between them: prototype and look, on a dark theme *and* a light one.

- Make the band read — raise the grey, or use an accent hue instead.
- Replace the band with a left-edge marker (`▌`). This carried a second argument:
  the full-width background is precisely what forces `reopenBand` to re-arm the
  band's SGR sequence after every inner cell reset, machinery that is subtle,
  invisible to tests, and the source of a real bug already (`DESIGN.md` §3). A
  marker needs none of it.

Four treatments were built behind `DRIFT_BAND` and looked at against a real repo.

## Decision

**Both.** The selected row is a subtle background band *under* a left-edge marker
— the shape fzf and Telescope both use. `reopenBand` stays.

The deciding property is degradation. A band alone is at the mercy of a background
Drift does not control and cannot fully detect; the failure mode that opened this
ADR is precisely a band disappearing into a theme nobody anticipated. A marker is
a glyph in an accent colour and does not depend on the background at all, so when
the band is at its weakest the row is still found. Neither half has to carry the
signal alone, which is why the pair beats a better-tuned version of either.

The band's own values are adaptive and pin **both** ends — background *and*
foreground. A background with no foreground was the second half of the same
defect: on a light terminal the default foreground is dark, so a dark band
rendered dark-on-dark, and the one thing that must always be legible was the thing
that disappeared.

## Consequences

- **`reopenBand` is permanent**, and the argument that a marker would delete it is
  spent. Any future row-assembling screen must keep the property it protects: a
  row is built from independently styled cells, each closing with a full SGR reset,
  and the band's sequence has to be re-armed after every one.
- **A second, new invariant joins it.** The marker needs a gutter, and the gutter
  is not the row's to spend. Rows size against `rowWidth` (the panel less the
  gutter) while the panel, the band and the chrome keep sizing against
  `contentWidth`. Getting this wrong is not cosmetic: a row built to the full panel
  and then pushed right overflows by exactly the gutter, and `clipRow` cuts the
  trailing cell — which on a branch row is the status cluster, the signal the whole
  area-15 allocation rule exists to protect.
- **The gutter is drawn on every row**, blank except the cursor's, so a marker
  never puts one row's columns out of line with the rest of the list.
- The cost is that Drift now carries both mechanisms where it might have carried
  neither's complexity in full. That is the price of the degradation property, and
  it is paid once in `band.go`, `view.go` and `window.go` rather than per screen.

## Alternatives rejected

- **`contrast`** (raised grey, both ends pinned) — the smallest change, and it
  needed no ADR since `DESIGN.md` §1 already calls the palette
  "considered-not-sacred". Rejected as the default only because it still depends
  entirely on the terminal's background being what we expect.
- **`accent`** (an accent hue band) — reads strongest at a glance, but the blue
  competes with the title and the checked-out marker, which are already blue (39).
- **`marker`** (marker alone, no band) — the one that would have deleted
  `reopenBand`. Rejected as too quiet: it marks where the cursor *is* without the
  row itself reading as selected, which is what a full row of columns needs.
- The original 236 band is **not a supported option**, not even as a choice. It is
  the defect; offering it would ship it.

## Notes

The alternatives remain in `band.go` and are selectable by `DRIFT_BAND`, which is
temporary and undocumented. A selection style is a per-user preference, and its
home is the user-global config root (roadmap area 16), which is where it goes when
that lands. This ADR decides the **default** and the mechanism, not that the choice
is forever unavailable.
