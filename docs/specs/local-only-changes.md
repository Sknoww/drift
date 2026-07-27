# Spec — Local-only changes

> Feature spec for roadmap **area 6**. Governs the rules and mechanics; the visual
> surface is ratified in [`DESIGN.md`](../../DESIGN.md), the architectural fit in
> [`CONTEXT.md`](../../CONTEXT.md). Read the unmergeable section of `CONTEXT.md`
> first — this feature deliberately copies its "teach Git the constraint, Git is the
> source of truth" shape.

## The problem

Working on a long-lived branch you accumulate edits you want on **this machine only**
and never in a commit: a bumped log level in a committed config, a local
`application.yml` tweak, a scratch script, a `docker-compose.override.yml`. Git's
defaults fight this — `git add -A` and `commit -am` sweep the tracked ones in; the
untracked ones clutter `git status` until one gets added by accident.

IntelliJ change lists solve it *inside the IDE*, and that boundary is the whole
weakness: a `git commit -am` in the terminal, CI, or a teammate's tooling ignores the
change list entirely. Drift sits one layer above Git, so it can hold these changes with
Git's **own** primitives — protection that doesn't stop at any boundary — and then do
the thing raw Git can't: make the held set **visible**.

## The two primitives

A local-only entry is a path. Drift routes it by whether Git tracks it — the user marks
a *change*, never a mechanism:

| Path is… | Held via | Git-native effect |
|---|---|---|
| **tracked** | `git update-index --skip-worktree -- <path>` | Git treats the file as unmodified no matter what you do to it: absent from `git status`, ignored by `add -A` and `commit -am` |
| **untracked** | a drift-fenced block in `$GIT_DIR/info/exclude` | the path is ignored locally, so it never shows as untracked and `add -A` can't stage it |

`info/exclude` is a per-repo, unversioned `.gitignore` Git already provides; it only
affects untracked paths (it cannot un-track something already tracked), which is exactly
its role here. Both files hang off the git dir drift already resolves via `gitDir()`.

## Git's flags are the source of truth

Copied verbatim from the unmergeable decision: **drift never keeps its own registry of
what is held.** The truth is read back from Git every load:

- tracked/held → `git ls-files -v`, lines tagged `S` (skip-worktree).
- untracked/held → the fenced block drift maintains in `$GIT_DIR/info/exclude`.

The store persists **only annotations** — a human note per path ("debug log level"),
nothing that could contradict Git. Consequences:

- drift can never fall out of sync with reality; a path un-held outside drift simply
  stops appearing, and its orphaned annotation is dropped on load.
- Git behaves correctly when drift isn't running — the whole point of teaching Git the
  constraint instead of routing around it.
- drift only ever edits **its own** fenced region of `info/exclude` (delimited by
  `# drift:begin` / `# drift:end`, the same fence the attributes file already uses —
  one Drift block shape wherever a user meets it), so hand-written exclude entries are
  never clobbered. Releasing the last held path removes the now-empty block outright,
  so the file goes back to exactly how the user had it.
- Each entry is written **anchored and escaped**: `/docker-compose.override.yml`, not
  the bare name. This is not cosmetic — a gitignore pattern containing no slash matches
  a *basename at any depth*, so an unanchored entry would quietly hold back every
  `config.yml` in the tree instead of the one the user picked. Glob metacharacters in a
  name are backslash-escaped for the same reason, and the encoding is reversed on read
  so the exclude file remains the single source of truth.

## It is repo/worktree-global, not per-branch

`skip-worktree` is an **index** flag, and one worktree has one index — so a held file
is held regardless of which branch is checked out. This is a property, not a limitation:
"keep my log tweak on every branch I touch" is the actual use case. The feature makes no
attempt to scope a hold to a ticket or branch, and the UI must not imply it does.

## Surviving the shelve sequence (area 7)

The headline promise: local-only changes **ride the shelve sequence untouched, with no
re-apply step.** This falls out of the primitives rather than being bolted on:

1. **`git stash`** captures only what Git *sees* as modified. Skip-worktree files look
   unmodified → never stashed. Untracked files aren't stashed without `-u` → left alone.
   Both categories stay physically in the working tree.
2. **merge target main** lands on top; the held edits sit undisturbed underneath.
3. **`git stash pop`** restores the *other* real work. The held files were never moved,
   so there is nothing to re-apply.

Drift's shelve sequence MUST use a plain `git stash` (no `-u`, no `-a`) for this to
hold. That is the only requirement the shelve step owes this feature.

### The one collision — surfaced, never silent

The single hazard, unavoidable under any mechanism: **the target main changed the very
file you hold locally.** Drift must not rely on Git's exact merge behavior here (it
varies by version — abort vs. clobber). Instead, **before merging**, drift computes the
incoming changed-file set and intersects it with the held set:

```
git diff --name-only <branch>...<targetRef>   # files the merge will touch
∩  held set (skip-worktree S-tagged + fenced exclude block)
```

Non-empty intersection → **halt before the merge**, hand back to the user, same shape
as an unmergeable handoff: drift surfaces the collision, the human reconciles, drift
never clobbers. This path-based check covers both categories uniformly, including the
edge where upstream newly *adds* a path you were keeping as an untracked local file.
Empty intersection (the common case) → fully automatic.

## Data model

The store gains a flat, repo-global annotation list — no ticket association, matching
the global nature of the hold:

```go
// LocalOnly annotates a path held back from commits. Git's own flags decide
// *whether* a path is held (skip-worktree bit for tracked, info/exclude for
// untracked); this records only the human context, and is reconciled against
// Git on every load so it can never contradict reality.
type LocalOnly struct {
    Path string // repo-relative
    Note string // why it's held, e.g. "debug log level" — optional
}
// Store gains:  LocalOnly []LocalOnly
```

`Kind` (tracked vs. untracked) is **derived at read time** from Git, never stored, so
a file that crosses the tracked/untracked line can't leave a stale label behind.

## Interaction rules (UI surface; visuals ratified in DESIGN.md)

A first-class **Local-only changes** view, not a footnote — visibility is the feature's
reason to exist. Rules the spec pins; DESIGN owns the pixels and must add the keys to
its keymap contract:

- The view lists every held path with its derived kind (tracked / untracked) and its
  note. The list is the source of "what am I hiding from Git," the thing raw
  `skip-worktree` can't give you.
- **Add** a currently-modified-but-not-yet-held change to the set (pick from working-tree
  changes; drift routes to the right primitive). Never auto-suggested — the user always
  chooses, per the project's "never guess" rule.
- **A staged change is refused, not held.** `skip-worktree` hides the *working tree* from
  Git; the index is what a commit writes. Holding a staged change would therefore look
  like protection and give none — the exact failure this feature exists to prevent. The
  candidate is listed, flagged, and blocked, with the fix named (`git restore --staged`).
- **Release** a held path: `--no-skip-worktree` (tracked) or remove the fenced exclude
  line (untracked). Releasing a tracked hold makes the local edits reappear as ordinary
  working-tree changes — they were never lost — leaving the user to commit or discard.
  Because it destroys nothing, release is one keystroke with no confirmation, unlike
  deleting a ticket. **"Release and discard" (`checkout -- <path>`) is deliberately not
  built:** it would be the only irreversible action in the feature, and `git` is one
  command away for anyone who wants it.
- **Edit note** on an entry.
- Held tracked files must be visually unmistakable *because* Git hides them; the view is
  the antidote to "I forgot that file was skipped."

Dashboard key `l` opens the manager — ratified in DESIGN's keymap contract.

## Area-1 (git wrapper) additions

- `SetSkipWorktree(ctx, path)` / `ClearSkipWorktree(ctx, path)` —
  `git update-index --skip-worktree | --no-skip-worktree -- <path>`.
- `SkipWorktreeFiles(ctx)` — `git ls-files -v`, keep lines tagged `S`, strip the tag.
- `ChangedFiles(ctx, branch, targetRef)` — `git diff --name-only <branch>...<targetRef>`
  for the collision check (likely shared with areas 5/7).
- A richer working-tree read than `IsDirty` (parse `git status --porcelain -z
  --untracked-files=all`) to offer candidates in the Add flow, tagging each with
  tracked/untracked (the routing) and staged (the refusal above). `--untracked-files=all`
  is load-bearing: Git's default collapses a wholly-untracked directory into one
  `service/` entry, and a hold is on a *file* — offering the directory would hold every
  path beneath it, including ones the user never saw.
- `SetSkipWorktree` / `SkipWorktreeFiles` must run **from the working-tree root**
  (`git -C <toplevel> …`): `update-index` resolves its filenames against the *current*
  directory, and `ls-files` reports only the directory it runs in — so a Drift invoked
  from a subdirectory would otherwise fail to hold a repo-relative path and would report
  only part of the held set. (`status --porcelain` needs no such help; its paths are
  root-relative by definition.)
- Exclude I/O is plain file read/append/rewrite on `$GIT_DIR/info/exclude`, off
  `gitDir()` — not a shell-out.

## Out of scope

- **Per-branch holds.** The index flag can't express them, and the use case doesn't want
  them.
- **Auto-detection** of what "should" be local-only. Always explicit.
- **Reconciling a genuine collision.** Drift surfaces it and hands to the human, exactly
  as with unmergeable files — it never merges the two versions.
- **Secret management.** Holding a file back from commits is not securing it.
- **`assume-unchanged`.** It's a performance hint Git may clear on its own — wrong tool
  for an intentional, durable override. `skip-worktree` only.
