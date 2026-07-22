# Spec — Unmergeable detection + diff panel

> Feature spec for roadmap **area 5**, first part (detection + diff). Governs the
> rules and mechanics; the visual surface is ratified in [`DESIGN.md`](../../DESIGN.md),
> the architectural fit in [`CONTEXT.md`](../../CONTEXT.md). **Read the unmergeable
> section of `CONTEXT.md` first** — this feature implements the hybrid detection rule
> defined there.
>
> **Split:** this part ships *reading* the unmergeable declaration and showing the
> diff. *Writing* the `-merge` attribute (to `.gitattributes` or
> `$GIT_DIR/info/attributes`, the user's choice) is the second part, deferred.

## The problem

Part of the codebase is files Git cannot merge — Unity scenes, `.pbxproj`, the
author's `.uwe` workflow files — reconciled by hand in an external tool. When a target
main moves under a branch and touches one of those files, the old workflow was: notice
the branch is behind, *guess* a file might have changed, open the web UI, and hunt for
what moved. The diff already exists locally after a fetch. Drift surfaces it.

## What counts as a collision

For one branch `B` paired to target `T` (`origin/<main>`), a file is an **unmergeable
collision** when all three hold:

1. **The target moved it.** The file is in `git diff --name-only B...T` — what `T`
   changed since the two diverged (their merge base). Empty unless `B` is *behind*, so
   detection is gated on `behind > 0` and reuses the count the sweep already computed.
2. **The branch changed it too.** The file is in the branch side — its committed
   changes (`git diff --name-only T...B`) **unioned with** the working-tree edits
   (`git diff --name-only HEAD`) for the checked-out branch only. Uncommitted local
   edits count: a plain `git stash` captures exactly them, so they are what turns into
   a conflict when the shelve sequence (area 7) pops them back over a merged target.
   Untracked files are excluded — plain stash leaves them in place, so they ride the
   merge through untouched.
3. **Git must never merge it** — the hybrid rule below.

Both-sides-changed is precisely the set a merge of `T` into `B` would conflict on, so
this predicts the conflict *before* the merge. A file only the target changed merges
cleanly and is never surfaced.

Only the checked-out branch can contribute working-tree edits — one worktree has one
index — so the branch side is non-uniform across a ticket's fan-out by design.

## Detection — hybrid, `.gitattributes`-first

A colliding path is unmergeable when **either** source says so (union, additive):

| Source | How | Owns |
|---|---|---|
| **Git's declaration** | `git check-attr -z merge -- <paths>`, path flagged when merge is `unset` (i.e. `-merge`) | `internal/git.CheckAttrMerge` |
| **Config globs** | `Config.Unmergeable[].Globs` matched with `doublestar` (`**` spans path segments) | `store.Config.MatchesUnmergeable` |

`-merge`, **not** the `binary` macro: `binary` implies `-diff`, which would kill the
diff panel. Unmergeable files are still text worth diffing.

`check-attr` is asked only about the (small) collision set, never the whole tree. A
malformed config glob is skipped, not fatal — one bad pattern must not blind the rest.

## What the panel shows

Per branch, the diff of each colliding file is its **incoming upstream change**:
`git diff B...T -- <path>` — exactly what the target changed since divergence, the text
to reconcile against. **Plain text for every format, always**; format-specific rendering
is a different product and out of scope (`DESIGN.md` §2).

Diffs load **lazily**: the sweep records only the colliding paths on each
`branchStatus`; each file's text is fetched on demand and cached when the panel opens,
so a branch with many collisions costs nothing until viewed. A fetched diff for a branch
the user has since left is discarded, never painted into the wrong branch's panel.

## Surface

- **Dashboard** — a branch with collisions shows a trailing `⚠ N unmergeable` marker
  (color role `unmerge`, `DESIGN.md` §1). Branch rows are individually selectable (the
  cursor moves over a flat visible-row list); `enter` on a branch opens its diff.
- **Diff panel** (`screenDiff`) — one branch's collisions: `tab`/`shift+tab` step
  between files, the diff scrolls in a viewport (`j`/`k`/arrows/pgup/pgdn), `esc` backs
  out. Reached per branch because MVP2 and MVP3 can hold different versions of the same
  file — a ticket-scoped diff would conflate them.

## Deferred to part 2

Writing the `-merge` attribute on request, to `.gitattributes` (committed, team-wide)
or `$GIT_DIR/info/attributes` (local, highest precedence) at the user's choice — see the
CONTEXT.md unmergeable section. Detection here only *reads* the attribute.

## Boundaries

- **Never** merges, auto-resolves, or edits an unmergeable file. The tool kills the
  work *around* the manual step; the reconciliation stays manual, permanently.
- Detection is meaningful only against fresh `origin/<target>` refs, so it rides the
  post-fetch sweep (it also runs on a plain refresh, harmlessly, against whatever refs
  are current).
