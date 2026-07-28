# drift

A ticket-based git TUI.

Drift sits one layer above git and organizes work by **ticket** instead of by flat
branch list. It's built for codebases with several long-lived "main" branches in
flight at once, where one ticket fans out into a feature branch per target main,
each based on a different version of the code. Git has no concept of that grouping,
and neither do general-purpose git tools — which is exactly why they bury you.

It also treats **unmergeable** files — Unity scenes, `.pbxproj`, Power BI workbooks,
notebooks, low-code exports — as a first-class concept rather than a special case,
and kills as much of the manual reconciliation dance as can be killed.

## Install

```sh
brew install Sknoww/tap/drift
```

With Go 1.24+ installed, either of these works too:

```sh
go install github.com/Sknoww/drift@latest   # straight to $GOBIN
go build -o drift .                         # from a checkout
```

## Getting started

Run `drift` from anywhere inside a git repo:

```sh
cd ~/your/repo
drift
```

The first run opens a **setup wizard** that lists the repo's own remote refs. Pick
the ones that are your long-lived targets with `space`, rename a target's short key
with `e`, and press `enter` to save. That writes `config.json` and drops you on the
dashboard. Decline with `esc` and drift tells you where to hand-edit the file instead.

From the dashboard, `a` adds a ticket: type its ID, then pair each of its branches
with the target that branch aims at. **The pairing is always yours to make** — drift
never guesses a target from a branch name, so any branch naming style works.

Press `?` on any screen for the keys and the glyph legend. The key half is generated
from the live keymap, so it always matches what the keys actually do.

## Reading the dashboard

Tickets are rows you expand into their branches:

```
▾ ABC-101  Fix the sync indicator
  ▸ ABC-101-main     → main    ↓4 ↑2  ●  ⚠ 2 unmergeable
    ABC-101-r2perf   → r2perf  ↓3 ↑2     ⚠ 2 unmergeable
▸ ABC-202  Tidy the settings pane
```

| Glyph | Meaning |
|---|---|
| `▸` / `▾` | ticket collapsed / expanded |
| `↓N` | commits the target has that you don't — **it moved** |
| `↑N` | commits you have that the target doesn't |
| `●` | uncommitted changes (checked-out branch only) |
| `▸` (on a branch) | the branch you have checked out |
| `⚠ N unmergeable` | both sides changed a file git can't merge — `enter` opens the diff |

## Keys

Defaults, per screen. All of them are named actions under the hood, so they're
rebindable without a code change.

**Dashboard**

| Key | Does |
|---|---|
| `j` / `k` / arrows | move |
| `enter` / `space` | expand a ticket · open a branch's diff |
| `a` | add a ticket |
| `d` | delete the selected ticket |
| `s` | shelve — stash, pull the target, merge it in, put your work back |
| `l` | manage local-only changes |
| `f` | fetch, then refresh (`esc` aborts an in-flight fetch) |
| `r` | refresh from git |
| `?` | help |
| `q` / `ctrl+c` | quit |

**Pairing a ticket's branches**

| Key | Does |
|---|---|
| `space` | include / exclude the selected branch |
| `t` | choose a target for it |
| `1`–`9` | assign the Nth configured target directly |
| `enter` / `esc` | save / back out |

**Diff panel** (`enter` on a branch with collisions)

| Key | Does |
|---|---|
| `tab` / `⇧tab` | next / previous colliding file (wraps) |
| `j` / `k` / arrows / pgup / pgdn | scroll the diff |
| `w` | declare this file's pattern unmergeable to git |
| `esc` | back to the dashboard |

**Local-only changes** (`l`)

| Key | Does |
|---|---|
| `a` | hold a working-tree change on this machine |
| `d` | stop holding the selected path |
| `n` | note why it's held |
| `esc` | back to the dashboard |

## What the features do

**Shelve** (`s`) runs the whole sequence on the selected branch in one keystroke:
stash → pull the target → merge it in → pop your work back on top. Unshelving *over*
the merge is the point — it means conflict markers never get written into a file that
can't be hand-edited. The report tells you where it stopped: `✓` done, `■` stopped and
handed back because there's something to reconcile, `✗` refused or failed outright.

**Unmergeable detection** is hybrid. Drift reads git's own declaration via
`git check-attr merge` — `*.uwe -merge` in a `.gitattributes` — and adds the glob
patterns from `config.json` on top. Use `-merge`, never the `binary` macro: `binary`
implies `-diff`, which would kill the diff panel, and these files are still text
worth diffing. `w` in the diff panel writes a declaration for you, either to the
committed `.gitattributes` at the repo root or to `$GIT_DIR/info/attributes` for a
local, unversioned one.

**Local-only changes** (`l`) hold a path back from commits using git's own flags —
the skip-worktree bit for a tracked file, `$GIT_DIR/info/exclude` for an untracked
one. Drift stores only your note about *why*, so its record can never contradict
reality. `◆` is tracked (git status won't show it), `◇` is untracked.

## Configuration

Two files live side by side under `<git-dir>/drift/`:

- **`config.json`** — yours to hand-edit. Targets, unmergeable classes, and where
  declarations may be written.
- **`state.json`** — drift's. Tracked tickets and local-only notes.

```json
{
  "targets": [
    { "key": "main",   "ref": "origin/main" },
    { "key": "r2perf", "ref": "origin/release-to-performance" }
  ],
  "unmergeable": [
    { "name": "workflows", "globs": ["workflows/**/*.uwe"] },
    { "name": "xcode",     "globs": ["**/*.pbxproj"] }
  ],
  "declare": {
    "destinations": ["local"]
  }
}
```

`targets` is unbounded — two is just this example. `declare.destinations` allow-lists
where `w` may write; omit the key entirely and both destinations are offered. A team
with no committed `.gitattributes` lists only `"local"`, and the shared destination
stops being offered at all, so it can never be picked by accident.

## Sandbox

`scripts/sandbox.sh` builds a throwaway repo that exercises everything drift does, so a
change can be tried against realistic state instead of against your own work.

```sh
scripts/sandbox.sh                                  # build it (also builds the binary)
cd ~/dev/repos/drift-sandbox/repo && ../drift       # run it
```

It creates a bare `origin`, two targets (`main`, and `r2perf` → `origin/release-to-performance`)
and four feature branches, each covering a case the dashboard has to get right:

| Branch | ↓↑ vs target | What it covers |
|---|---|---|
| `ABC-101-main` | ↓4 ↑2 | 2 unmergeable — one committed, one **working-tree only** |
| `ABC-101-r2perf` | ↓3 ↑2 | 2 unmergeable, one per detection half (config glob / `.gitattributes`) |
| `ABC-202-main` | ↓4 ↑1 | behind, but everything it touched merges — **no marker** |
| `ABC-303-main` | ↓0 ↑0 | in sync, so detection is skipped entirely |

It leaves you on `ABC-101-main` with an uncommitted edit, which is what makes that
branch's second collision appear — commit or stash it and the count drops to 1.
`.gitattributes` declares `*.pbxproj -merge`, so git's own half of the hybrid rule is
live from the start, and the config allow-lists `"local"` as the only declare
destination.

Re-run it any time to reset; it is idempotent, and it refuses to wipe a directory that
is not already one of its sandboxes. Pass a path to build it somewhere else, or
`--wizard` to leave the repo unconfigured so the first-run wizard opens instead of the
dashboard.

## License

MIT
