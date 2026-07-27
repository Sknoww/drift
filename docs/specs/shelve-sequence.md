# Spec — One-key shelve sequence

> Feature spec for roadmap **area 7**. Governs the rules and mechanics; the visual
> surface is ratified in [`DESIGN.md`](../../DESIGN.md), the architectural fit in
> [`CONTEXT.md`](../../CONTEXT.md). Read the unmergeable section of `CONTEXT.md` and
> [`local-only-changes.md`](./local-only-changes.md) first — this feature is where both
> of them cash out. It is also the first thing Drift does that **writes to the repo's
> working tree and history**, so the rules about when it may do so are the substance of
> this spec.

## The problem

Your target main moved. To pick the move up you must: stash your work, pull the target,
merge it into your branch, pop your work back — and do it again for the next branch, and
the next. It is four commands you have typed a thousand times, in an order you must not
get wrong, where the interesting part (a conflict in a file no tool can merge) shows up
somewhere in the middle and demands you stop and think.

Drift already knows every piece of that sentence: which branch, which target, whether
it's behind, which files are unmergeable, which files you hold locally. The sequence is
the payoff for storing the grouping in the first place — **one keypress per branch**,
and it either lands or it leaves you exactly where you were.

## The sequence

Run against the **checked-out** branch and its paired target. Steps 1–3 mutate nothing.

| # | Step | Mutates |
|---|---|---|
| 0 | **Preconditions** — see below. Any failure stops here with nothing done | no |
| 1 | **Pull the target** — `git fetch <remote> <branch>` for this target's ref only | refs |
| 2 | **Recompute behind** against the freshly-updated ref. `behind == 0` → *already current*, stop | no |
| 3 | **Local-only collision check** — incoming changed files ∩ held set. Non-empty → halt | no |
| 4 | **Stash** — plain `git stash push` (no `-u`, no `-a`) | tree, index |
| 5 | **Merge** — `git merge --no-edit <targetRef>`. Conflict → abort and restore (below) | tree, HEAD |
| 6 | **Pop** — `git stash pop --index`. Conflict → halt in place (below) | tree, index |
| 7 | **Refresh** the branch's status so the row shows the new reality | no |

### Read-only until the last possible moment

The ordering above is the spec's central mechanic, not an implementation detail. Every
check that can refuse the sequence — the preconditions, "is there even anything to
merge", the held-set collision — runs **before the stash**, which is the first step that
touches the working tree. The consequence: a sequence that refuses has stashed nothing,
merged nothing, and has nothing to undo. There is no such thing as a partially-applied
refusal.

This is why the fetch comes first and the `behind` count is recomputed *after* it. A
sequence that stashed, fetched, discovered `behind == 0`, and popped back would have
churned the working tree to accomplish nothing — and every such churn is a chance to
leave the user somewhere they did not ask to be.

### Preconditions

Checked in this order; the first failure is reported and stops the sequence:

- **HEAD is on a branch.** Detached HEAD has nothing to merge into.
- **The selected branch is the checked-out one.** See *Scope* below.
- **No operation already in progress** — no `MERGE_HEAD`, `CHERRY_PICK_HEAD`,
  `REVERT_HEAD`, `rebase-merge/`, or `rebase-apply/` under the git dir. The repo is
  already mid-something; Drift will not stack a sequence on top of it.
- **The branch has a paired target**, and that target resolves to a real ref.

## Scope: the checked-out branch only

`s` runs on the branch you are on. On any other branch row it reports *"not checked out
— `git switch <branch>` first"* and does nothing.

This preserves Drift's standing invariant: **it never checks anything out.** The reason
is not squeamishness, it is correctness — a stash belongs to the branch it was taken on.
A cross-branch sequence would have to `checkout` → merge → `checkout` back → pop, and
every arrangement of those steps either carries uncommitted work across a branch
boundary or pops it onto the wrong tree. The roadmap's "one keypress per branch" means
*per branch, as you get to it*, not *all of them from where you stand*.

Auto-checkout is not refused forever, only deferred with its questions open: what
happens to uncommitted work on the branch you are leaving, and whether Drift wants to
own the "put me back where I was" bookkeeping that implies. It would earn an ADR, since
it reverses a documented invariant.

## Pulling the target

Drift compares against `origin/<target>` and never checks a target out, so "pull the
target main" means: **update that remote-tracking ref, then merge it.** That is what
`git pull` is; Drift just does the two halves against a ref it never has to visit.

The fetch is **scoped to the one target being merged** — `git fetch <remote> <branch>`,
not the whole remote. It is faster, and it cannot quietly change the ahead/behind
numbers of every *other* branch on screen partway through a sequence the user started
for one of them. It runs on a cancellable context, exactly like area 3's `f`.

Deriving `<remote>` and `<branch>` from `Target.Ref` is done by **asking git**, never by
splitting on `/` and hoping: the first path component is a remote only if `git remote`
lists it, and branch names contain slashes routinely. If the target ref is not
remote-tracking (a local ref, or a remote Drift cannot identify), there is nothing to
pull — the fetch step is **skipped and said so**, and the sequence continues against the
ref as it stands. A target that cannot be fetched is not an error; silently pretending
it was fetched would be.

## Halts

Every halt is a **handoff**: Drift stops, names what it found, and leaves the
reconciliation to the human. That is the same rule as an unmergeable file, and it is
permanent.

### Held-file collision (step 3) — before anything is touched

The hazard [`local-only-changes.md`](./local-only-changes.md) names: the target main
changed a file you hold on this machine. Drift does not rely on Git's behavior here (it
varies by version — abort vs. clobber), it checks first:

```
git diff --name-only <branch>...<targetRef>    # what the merge will bring in
∩  held set (skip-worktree `S`-tagged + fenced exclude block)
```

Non-empty → halt, list the colliding paths with their notes, and stop. Nothing has been
stashed or merged. The user releases the hold, or reconciles by hand, and re-runs.

Empty (the common case) → the held set rides the rest of the sequence untouched, with no
re-apply step, because a plain `git stash` cannot see it. **Step 4 must never grow a
`-u` or `-a` flag**; that is the one thing this sequence owes area 6, and adding either
would silently break the promise.

### Merge conflict (step 5) — abort and restore

A conflict here means both sides *committed* to the same file. Drift runs
`git merge --abort`, then `git stash pop --index`, and reports the conflicting paths —
flagging which of them are unmergeable, since that is what determines whether the
reconciliation is a text merge or a trip to an external tool.

**The sequence is therefore atomic across its mutating steps: it either lands whole, or
it leaves no trace.** The user ends up byte-for-byte where they started, holding a list
of what they are about to have to deal with. Nothing is half-merged, and nothing is
sitting in a stash they have to remember about.

If `merge --abort` itself fails, Drift **halts without popping.** A failed abort means
the repo is not in the state Drift thought it was, and stacking a pop on top of that
turns one problem into two. It reports the raw git error and stops.

### Pop conflict (step 6) — halt in place, stash retained

Your uncommitted work vs. the target's newly-merged content. This is the case the whole
feature exists for: with the unmergeable file's local edit safely in the stash, the merge
itself was clean, and the conflict surfaces here — over a `-merge` file, with no conflict
markers ever written into it, which is precisely the flow `CONTEXT.md` describes.

**This halt does not restore, and the asymmetry with step 5 is deliberate.** `git stash
pop` does not drop the stash entry when it conflicts, so nothing is at risk: the work is
still in the stash, the merge is committed, and the user is standing exactly at the
reconciliation point they ran the sequence to reach. Auto-restoring here would undo the
one thing that went right and make Drift unable to ever deliver its own headline flow.

Drift reports: the conflicting paths (unmergeable ones flagged), that the merge landed,
and that **the stash was retained** — so "did I lose my work" is answered on screen
rather than in a support thread. Reconciliation stays manual, always.

## Stash identity — never `stash@{0}` on faith

Two failure modes that a naive implementation hits and a user only discovers afterward:

- **`git stash push` with nothing to stash succeeds and creates no stash.** It prints
  "No local changes to save" and exits 0. A sequence that pops unconditionally would then
  pop *someone else's* stash — an unrelated pile of work, applied onto a branch it was
  never taken from. Drift resolves `refs/stash` before and after the push; unchanged (or
  still absent) means nothing was stashed, and step 6 is skipped entirely.
- **`stash@{0}` is a position, not an identity.** Anything that stashes concurrently —
  another terminal, an IDE, a hook — shifts it. Drift records the stash **commit OID**
  created in step 4 and verifies `refs/stash` still points at it before popping. If it
  does not, Drift **refuses to pop**, names the OID it was looking for, and tells the user
  their work is in the stash list under Drift's message. Popping the wrong stash is not a
  recoverable mistake in the way most git mistakes are.

The stash is created with an identifying message — `drift: shelve <branch> ← <targetKey>`
— so the entry is findable by eye in `git stash list` on any path where Drift hands back.

`pop --index` is used unconditionally. With nothing staged it behaves identically to a
plain pop; with something staged it is what preserves the staged/unstaged split the user
built, which a plain pop would flatten into one unstaged pile.

## Git must never open an editor

The merge runs `--no-edit`, and every git invocation in this sequence runs with an
environment that cannot spawn an editor (`GIT_EDITOR=true`, and the `core.editor`
override that closes the same door). Drift owns the terminal; a git subprocess launching
`vim` into the middle of a Bubble Tea render corrupts the display and strands the user
in an editor they did not ask for and may not know how to leave.

## Interaction rules (UI surface; visuals ratified in DESIGN.md)

- **`s` on a branch row** runs the sequence — named action `shelve`. It is offered
  (and its key documented in the `?` overlay) on the dashboard.
- **One at a time.** While a sequence is in flight, a second `shelve` is refused, not
  queued. Two concurrent stash/merge sequences against one index is not a state worth
  being able to reach.
- **The running sequence is visible step by step** — the user must be able to see which
  of fetch / check / stash / merge / pop is happening. A sequence that mutates the
  working tree behind a single undifferentiated spinner gives the user nothing to reason
  about when it stops.
- **Cancellation applies to step 1 only.** `esc` kills an in-flight fetch, as it already
  does on the dashboard. Once the stash is taken the sequence runs to a halt or to
  completion; there is no cancelling into an undefined middle.
- **Quitting mid-sequence is allowed, deliberately.** `q` and `ctrl+c` are not trapped
  while the mutating steps run: an escape hatch you can be locked out of is worse than
  the state it protects. And that state is already handled — a half-finished merge is
  what the `OperationInProgress` precondition exists to detect, so the next `s` refuses
  and names it rather than stacking on top. The failure mode is self-healing, which is
  exactly why it does not justify trapping the user.
- **Every halt names its next action** — the git command that resolves it, in the same
  register as area 6's staged-change refusal (`git restore --staged`). Drift's job is to
  hand back a well-lit problem, not a stack trace.
- A held-file collision may reuse the **area-5 diff panel** to show what the target did
  to each colliding path. Natural, and the panel already exists — but not required for
  the area to be done.

`s` does not collide with any bound key on the dashboard.

## Area-1 (git wrapper) additions

- `Remotes(ctx)` — `git remote`; the lookup that makes remote/branch splitting safe.
- `FetchRef(ctx, remote, branch)` — `git fetch --quiet <remote> <branch>`, cancellable.
- `Stash(ctx, message)` — `git stash push -m <message>`, returning the created stash OID
  and whether anything was stashed at all (resolve `refs/stash` before and after).
- `StashRef(ctx)` — `git rev-parse --verify --quiet refs/stash`; absent is not an error.
- `StashPop(ctx)` — `git stash pop --index`, distinguishing clean / conflicted / failed.
- `Merge(ctx, ref)` — `git merge --no-edit <ref>`, distinguishing already-up-to-date /
  fast-forward / merged / conflicted, with the conflicting paths on the last.
- `MergeAbort(ctx)` — `git merge --abort`.
- `ConflictedFiles(ctx)` — `git diff --name-only --diff-filter=U`.
- `OperationInProgress(ctx)` — the `MERGE_HEAD` / `CHERRY_PICK_HEAD` / `REVERT_HEAD` /
  `rebase-merge` / `rebase-apply` check, read as file existence off `gitDir()` in the
  same file-level style as the exclude and attributes I/O.
- `ChangedFiles` (area 5) already serves the collision check; `AheadBehind` (area 1)
  already serves the recompute. Neither needs changing.

Every one of these takes a `context.Context` and runs through the existing `run` helper,
so the editor-proof environment above is set in **one** place rather than per call site.

## Out of scope

- **Cross-branch and batch shelving.** One branch, the checked-out one, per keypress.
- **Reconciling anything.** Drift surfaces conflicts and hands to the human — the
  permanent rule from `CONTEXT.md`, and the reason the sequence has halts at all.
- **Committing the resolution**, staging resolved files, or `merge --continue`. Once
  Drift has handed back, the repo is the user's again.
- **Rebase.** `CONTEXT.md` pins merge; a rebase replays your commits *through* the
  unmergeable file repeatedly, which is the failure mode shelving exists to avoid.
- **`git stash -u` / `-a`.** Would sweep up exactly the local-only changes area 6 holds.
- **Stash management** — listing, dropping, or applying stashes Drift did not create.
  Drift creates one stash and pops the same one; `git` owns the rest.
- **Pushing.** The sequence brings the target *in*; it never sends anything out.
