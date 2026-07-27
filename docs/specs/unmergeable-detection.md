# Spec — Unmergeable detection, diff panel, and declaring

> Feature spec for roadmap **area 5**. Governs the rules and mechanics; the visual
> surface is ratified in [`DESIGN.md`](../../DESIGN.md), the architectural fit in
> [`CONTEXT.md`](../../CONTEXT.md). **Read the unmergeable section of `CONTEXT.md`
> first** — this feature implements the hybrid detection rule defined there.
>
> Built in two parts, both now shipped: *reading* the unmergeable declaration and
> showing the diff, then *writing* the `-merge` attribute on request.

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

Plain text is not uncolored. The diff is colored by **line role** — added, removed, hunk
header, git's bookkeeping — because distinguishing an added line from a removed one is
what reading a diff is, whatever the file happens to be. What stays permanently out of
scope is understanding the *format*: no Unity scene tree, no workflow graph. The
classification is a pure function (`diffLineRole`) so it is testable, since under a test's
ASCII color profile every style renders identically — which would otherwise hide the one
real trap here, that a `+++`/`---` file header must be matched before the bare `+`/`-`
it starts with.

Diffs load **lazily**: the sweep records only the colliding paths on each
`branchStatus`; each file's text is fetched on demand and cached when the panel opens,
so a branch with many collisions costs nothing until viewed. A fetched diff for a branch
the user has since left is discarded, never painted into the wrong branch's panel.

## Surface

- **Dashboard** — a branch with collisions shows a trailing `⚠ N unmergeable` marker
  (color role `unmerge`, `DESIGN.md` §1). Branch rows are individually selectable (the
  cursor moves over a flat visible-row list); `enter` on a branch opens its diff.
- **Diff panel** (`screenDiff`) — one branch's collisions: `tab`/`shift+tab` cycle
  between files (wrapping at both ends), the diff scrolls in a viewport
  (`j`/`k`/arrows/pgup/pgdn), `esc` backs out. Reached per branch because MVP2 and MVP3 can hold different versions of the same
  file — a ticket-scoped diff would conflate them.
- **Declare overlay** — `w` on the diff panel, drawn in the panel's place like the
  target picker. Two steps, `esc` unwinding one at a time. While it is open every key is
  bound, so `j`/`k` move the cursor rather than scrolling the diff underneath.

## Declaring — writing the `-merge` attribute

Detection *reads* the attribute; declaring *writes* it, so Git behaves correctly on a
merge even when Drift isn't running. `w` on the diff panel opens a two-step overlay for
the file on screen — the file is already one Drift knows must never be merged, so the
question is only what to declare and where.

**What gets written.** Both are offered and the user picks; Drift never chooses:

| Choice | Pattern | Covers |
|---|---|---|
| A matched config glob | `workflows/**/*.uwe` | the whole class, in one line |
| The file's own path | `workflows/mvp2/flow.uwe` | just this file |

Every config glob that matched the path is listed first (tagged with its class name, so
the *why* is visible), then the path itself. A file flagged only by `check-attr` matches
no glob, so it is offered its path alone — the list is never empty.

**Where it goes** — the user's choice, both first-class (`CONTEXT.md`):

| Destination | Config name | Consequence |
|---|---|---|
| `<toplevel>/.gitattributes` | `shared` | committed, shared with the team; needs commit rights |
| `$GIT_DIR/info/attributes` | `local` | local, unversioned, highest precedence of any attributes source |

A repo can **allow-list** destinations in `config.json`, which filters *and* orders the
picker:

```json
"declare": { "destinations": ["local"] }
```

Omit the key and both are offered. A team that keeps no committed `.gitattributes` lists
only `"local"`, and the shared destination stops being offered at all — it cannot be
picked by accident, and Drift can never dirty a file the team does not use. It lives in
`config.json` rather than behind a keypress on purpose: a guard against an unwanted
commit is worth more when it cannot be toggled off by a stray keystroke. An unknown or
duplicated name, or a present-but-empty list, is a **validation error** rather than
something skipped over — silently ignoring a typo would leave `shared` on offer for the
very person who wrote the key to be rid of it.

### Making it visible

Declaring a file the panel *already* flags as unmergeable changes nothing on screen
unless the panel says which half of the hybrid rule flagged it. So each collision
carries whether **Git itself** knows (`-merge` set) as against only Drift's config
globs, and the panel badges the current file with it:

| State | Badge |
|---|---|
| Git has been told | `✓ declared to git` |
| Only Drift's config knows | `not declared to git — w declares it` |

`w` changes that state and the badge flips — the confirmation that something happened.
After a write, Drift **re-reads** `check-attr` for the panel's files rather than assuming
what its own write achieved, so a glob that covers several listed files flips all of
them, and the badge can never drift from what Git actually thinks.

**Mechanics** (`internal/git/attributes.go`):

- `-merge`, never the `binary` macro — the same rule detection follows, for the same
  reason: `binary` implies `-diff` and would kill the diff panel.
- Lines land inside a **Drift-fenced block** (`# drift:begin` … `# drift:end`), so
  Drift's writes are identifiable and removable, and a repeat declaration joins its
  siblings instead of scattering down a file the user also hand-maintains. A file with
  no fence gets a fresh block appended; a half-written fence is hand-mangled and left
  alone, with a complete new block added after it.
- **Idempotent.** A pattern already declared `-merge` *anywhere* in that file — inside
  the fence or hand-written above it — is reported back as already declared and nothing
  is written. Declaring twice is a no-op, never a duplicate line.
- Nothing else in the file is reordered or rewritten, and the write is atomic
  (temp-file + rename), the same guarantee the store gives its JSON.
- A pattern containing whitespace is quoted, which is how Git's own attributes syntax
  carries one.
- This is the one place the git layer touches a file instead of shelling out — an
  attributes file is plain text with no plumbing to write it. The file's *location* is
  still asked of Git (`TopLevel`, `GitDir`), never assembled by hand, so a linked
  worktree or submodule stays correct.
- A repo-root write dirties the working tree, so it triggers a plain re-sweep and the
  dashboard's dirty dot stays honest. A local write touches nothing Git tracks.

## Boundaries

- **Never** merges, auto-resolves, or edits an unmergeable file. The tool kills the
  work *around* the manual step; the reconciliation stays manual, permanently.
- Detection is meaningful only against fresh `origin/<target>` refs, so it rides the
  post-fetch sweep (it also runs on a plain refresh, harmlessly, against whatever refs
  are current).
