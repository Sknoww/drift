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

Most refs arrive with a key already filled in, taken from the ref's name. A ref with a
deeper path — `origin/fix/ABC-123-something/mvp-3` — arrives with **no** key and says
`name it (e)`, because there is no short name drift can honestly cut out of that: the
last segment would read `mvp-3`, which is a real target's name on somebody's ticket
branch. Drift never offers a key it can't stand behind, so that one is yours to type.

From the dashboard, `a` adds a ticket: type its ID, then pair each of its branches
with the target that branch aims at. **The pairing is always yours to make** — drift
never guesses a target from a branch name, so any branch naming style works.

Press `?` on any screen for the keys and the glyph legend. The key half is generated
from the live keymap, so it always matches what the keys actually do.

## Reading the dashboard

Tickets are rows you expand into their branches:

```
▾ ABC-101  Fix the sync indicator
  ▸ ABC-101-main     → main    ↓4 ↑2  ⇡ ●  ⚠ 2 unmergeable
    ABC-101-r2perf   → r2perf  ↓3 ↑2      ⚠ 2 unmergeable
▸ ABC-202  Tidy the settings pane
```

| Glyph | Meaning |
|---|---|
| `▸` / `▾` | ticket collapsed / expanded |
| `↓N` | commits the target has that you don't — **it moved** |
| `↑N` | commits you have that the target doesn't |
| `⇡` | commits not yet on `origin/<branch>` — `u` publishes them |
| `⊘` | no upstream — nothing to publish to yet |
| `●` | uncommitted changes (checked-out branch only) |
| `▸` (on a branch) | the branch you have checked out |
| `⚠ N unmergeable` | both sides changed a file git can't merge — `enter` opens the diff |

`↑N` and `⇡` count against **different** remotes, and it is the one place on the
row where the denominator changes: `↑N` is how far you are ahead of the *target*,
`⇡` is how far you are ahead of *your own branch's* remote. A branch can be level
with its target and still unpublished — which is exactly the state `s` leaves
behind and `u` clears.

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
| `u` | update — bring the selected branch up to date and publish it |
| `s` | shelve — merge the target into this branch, here; nothing is published |
| `l` | manage local-only changes |
| `t` | show the configured targets and the refs they point at |
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

**Targets** (`t`)

| Key | Does |
|---|---|
| `e` | point the selected target at a different ref (`/` filters, then `y` confirms) |
| `esc` | back to the dashboard |

## What the features do

**Shelve** (`s`) runs the whole sequence on the checked-out branch in one keystroke:
stash → pull the target → merge it in → pop your work back on top. Unshelving *over*
the merge is the point — it means conflict markers never get written into a file that
can't be hand-edited. The report tells you where it stopped: `✓` done, `■` stopped and
handed back because there's something to reconcile, `✗` refused or failed outright.

**Update** (`u`) is the same merge, carried the whole way, on *any* paired branch —
including one you don't have checked out. It stashes, checks the branch out, pulls the
branch's own upstream, merges the target, pushes, then puts you back on the branch you
were standing on and pops your work. Every halt unwinds the same way, so a conflict
still leaves you where you started.

The two differ by **commitment**. `s` merges the target in and publishes nothing, which
leaves the branch ahead of its own remote — `⇡` on the dashboard, and useful when you
want to look at the merge before it goes anywhere. `u` finishes the job and clears it.
Nothing is ever force-pushed: a rejected push means someone else's commit is in the way,
so Drift leaves the branch merged locally and tells you.

`u` **asks first, every time** — one `y`/`n` over the plan, before a single ref is
touched. It names the steps in run order, and it names the **ref** it is about to merge
(`origin/release/mvp-3`), not just the target's key, plus where the push will land. That
matters because a target's key is a label you chose and its ref is what actually gets
merged: a key reading `mvp-3` pointing at somebody's ticket branch looks perfectly
correct on the dashboard, and the prompt is where the two are shown together. The push is
also the one step with no undo and the only one other people see, so it never happens on
a keypress that wasn't told it would.

When there's uncommitted work on the tree, the plan says where it goes too: stashed on
the branch being left and popped on that same branch, never on the one Drift visits, and
that holds on every halt path. `s` never prompts — it publishes nothing, so everything it
does is local and the same unwind covers it.

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

Two files live side by side under `<git-dir>/drift/`, and one — optional — lives with
your other tools' config, in `~/.config/drift/`.

### Per repo

- **`config.json`** — yours to hand-edit, except for a target's `ref`, which `t` then `e`
  re-points from inside drift. Targets, unmergeable classes, and where declarations may be
  written.
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

`targets` is unbounded — two is just this example. A target's `ref` is the one thing here
you don't have to edit by hand: `t` shows what each target actually points at, and `e`
points one somewhere else, picked from the repo's own refs. That matters because a `key`
can read perfectly while the `ref` behind it names the wrong branch — and until you look,
every `↓behind` on the dashboard is measured against that wrong branch. `declare.destinations` allow-lists
where `w` may write; omit the key entirely and both destinations are offered. A team
with no committed `.gitattributes` lists only `"local"`, and the shared destination
stops being offered at all, so it can never be picked by accident.

### Per user

**`~/.config/drift/prefs.json`** — optional, and absent on most machines. Preferences
belong to you rather than to a checkout, so they live here instead of being re-declared
in every repo you clone. `$XDG_CONFIG_HOME` is honoured where it's set.

```json
{
  "selection": "pair",
  "accent": "#ff8800",
  "background": "dark"
}
```

Every key is optional, and no file at all is the full default set.

`selection` is the **shape** of the cursor's row:

| value | |
|---|---|
| `pair` | a subtle band under a left-edge `▌` — the default |
| `contrast` | a grey band that reads on its own, no marker |
| `accent` | a fixed accent-hue band rather than a lighter grey |
| `marker` | the left-edge `▌` only, no background at all |

`accent` is the **colour** poured into it — one value, either an ANSI-256 index
(`"39"`) or a hex colour (`"#ff8800"`). It recolours the three things that mean "drift
is pointing at this": the title, the checked-out branch marker, and the selected row's
`▌`. They move together on purpose.

Nothing else is themable. Colour is the signal here — `↓behind` is the one thing on
screen that shouts and `⚠ unmergeable` is a distinct alarm beside it — so a setting
that let two of those collide wouldn't be a preference, it'd be a broken screen.

> `selection: "accent"` and the `accent` key are unrelated, which the shared word
> hides. The treatment is a fixed-hue *background*, and a background is only half a
> decision: it needs a foreground pinned against it, and one value can't pin a pair.
> The `accent` key colours foregrounds, where one value carries the signal on its own.

`background` forces which end of the palette is used when your terminal is misdetected
— `"light"` or `"dark"`. Drift normally reads it from the terminal, and every colour
names both ends, so this is only for a terminal that reports the wrong thing.

An unrecognized value on any of the three is an error naming the file, not a silent
fall back to the default — a preference that quietly didn't apply looks exactly like
one that did.

Each has a single-run override, which is how to try one before saving it:

```sh
DRIFT_BAND=marker drift
DRIFT_ACCENT='#ff8800' drift
DRIFT_BG=light drift
```

An env var says "for this run" in a way an edited file can't — you'd be editing the
file whose contents you're trying to decide. While any of them is set the title names
what's actually in force, including which background was detected; a typo in one falls
through to the file rather than refusing, and the title still names what really
rendered.

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
