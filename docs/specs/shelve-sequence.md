# Spec — The one-key sequences: shelve and update

> Feature spec for roadmap **areas 7 and 17**. Governs the rules and mechanics; the
> visual surface is ratified in [`DESIGN.md`](../../DESIGN.md), the architectural fit in
> [`CONTEXT.md`](../../CONTEXT.md). Read the unmergeable section of `CONTEXT.md` and
> [`local-only-changes.md`](./local-only-changes.md) first — this feature is where both
> of them cash out. It is also the only thing Drift does that **writes to the repo's
> working tree and history**, so the rules about when it may do so are the substance of
> this spec.
>
> Area 17 reversed this spec's original *Scope* section, under
> [ADR 0002](../adr/0002-update-checks-out.md). What that section said — the sequence
> runs on the checked-out branch only — is still true of `s` and no longer true of `u`.

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

## Two verbs, differing by commitment

One state machine runs both. They share every halt, the stash identity rules and the
report; what differs is how far each is willing to go.

| | `s` shelve | `u` update |
|---|---|---|
| Which branch | the checked-out one | any paired branch |
| Pulls the target | yes | yes |
| Pulls the branch's own upstream | no | yes |
| Pushes | **no** | yes |
| Leaves you | where you were | where you were |

`s` leaves the branch **ahead of its own remote**, and that is its point, not an
oversight: it is the path for seeing a merge before it goes anywhere. `u` finishes the
job. A verb that has shipped is a verb someone has, so `s` keeps its original scope
exactly; its refusal on another branch's row now names `u` rather than only `git switch`.

Because the `?` overlay's key table is generated per action, the two one-line
descriptions have to carry the distinction on their own — "merge the target into this
branch, nothing is published" against "bring the selected branch up to date and publish
it". If that cannot be said in one line each, the split is wrong.

## The sequence

Steps 0–3 mutate nothing. Steps marked *update* are skipped by `s`.

| # | Step | Mutates |
|---|---|---|
| 0 | **Preconditions** — see below. Any failure stops here with nothing done | no |
| 1 | **Fetch the target** — `git fetch <remote> <branch>` for this target's ref only | refs |
| 1b | *update:* **fetch the branch's own upstream**, the same way | refs |
| 2 | **Recompute the divergence** against the freshly-updated refs. Nothing to do → stop | no |
| 3 | **Local-only collision check** — everything incoming ∩ held set. Non-empty → halt | no |
| 4 | **Stash** — plain `git stash push` (no `-u`, no `-a`). Clean tree → nothing created | tree, index |
| 5 | *update:* **check the branch out**, if it is not already current | tree, HEAD |
| 6 | *update:* **merge the branch's own upstream**. Conflict → the branch diverged from itself | tree, HEAD |
| 7 | **Merge the target** — `git merge --no-edit <targetRef>`. Conflict → roll back (below) | tree, HEAD |
| 8 | *update:* **push** — never forced. Rejected → a handoff, not a failure | remote |
| 9 | *update:* **return** to the branch the user was standing on | tree, HEAD |
| 10 | **Pop** — `git stash pop --index`. Conflict → halt in place (below) | tree, index |
| 11 | **Refresh** the branch's status so the row shows the new reality | no |

Both fetches are **hoisted above the stash**, and that is not an optimisation — it is
what keeps the read-only rule below true for a sequence that merges two refs instead of
one. Fetching is how the numbers step 2 refuses on become true, and none of it touches
the working tree.

### What "nothing to do" means, per verb

For `s`: the target hasn't moved (`behind == 0`). For `u` it takes all three — the
target hasn't moved, the branch is level with its own upstream, and there is nothing to
publish. Half of "this branch is up to date" is a claim about the remote, so a branch
level with its target but ahead of its own upstream is not done.

A branch with **no upstream at all** is a third answer, and reporting it as up to date
is the one claim `u` must never make: there is nothing to pull into it and nowhere to
publish it, so the sequence says so and names `git push -u`.

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

- **HEAD is on a branch.** Detached HEAD has nothing to merge into, and for `u` it is
  also nowhere to come back to — the return is part of the sequence, so a starting point
  that is not a branch is refused rather than approximated.
- **For `s` only: the selected branch is the checked-out one.** See *Scope*.
- **No operation already in progress** — no `MERGE_HEAD`, `CHERRY_PICK_HEAD`,
  `REVERT_HEAD`, `rebase-merge/`, or `rebase-apply/` under the git dir. The repo is
  already mid-something; Drift will not stack a sequence on top of it. This precondition
  also earns its keep later: anything the rollback finds in flight is necessarily
  Drift's own.
- **The branch has a paired target**, and that target resolves to a real ref.
- **For `u` only: if the tree is dirty *and* the branch is not already checked out, the
  user confirms.** Not a precondition that can fail — a prompt. See *The dirty tree, and
  where it splits*.

## Scope

`s` runs on the branch you are on. On any other branch row it reports *"not checked out
— press `u` to update it, or `git switch <branch>`"* and does nothing.

`u` runs on any paired branch, checking it out and putting you back afterwards. That
reverses the standing "Drift never checks anything out" invariant, under
[ADR 0002](../adr/0002-update-checks-out.md).

The invariant's *reason* is preserved rather than discarded. **A stash belongs to the
branch it was taken on**, and it still does: Drift stashes on the branch it is leaving
and pops on that same branch once it has returned, so uncommitted work never crosses a
branch boundary. What changed is that the guarantee is enforced by the **return**
instead of by never leaving.

### You end up where you started

The branch list is a list, not a place you move to. Updating five branches must not
silently relocate you, and each `u` has to start from the same known place as the last.
So the return is part of the sequence rather than a courtesy, and **every halt path
unwinds too** — a conflict at step 6, 7 or 8 still puts you back and still pops.

The unwind runs in one order: roll the merge back, return, then pop. It stops at the
first failure and reports it rather than stacking the next step on top — a rollback
already not going to plan is exactly when carrying on turns one problem into two.

The one thing it will not do is pop when it could not get back. Popping a stash wherever
Drift happens to be standing is the single thing this whole arrangement exists to
prevent, so a failed return reports the two commands that finish it by hand and stops.

### The dirty tree, and where it splits

Conflating these two is what made cross-branch work look harder than it is:

- **Same branch, dirty** — exactly today's shelve. Stash and pop happen on one branch,
  no boundary is crossed, and it is already atomic. It needs no new argument: it only
  gains the upstream pull and the push.
- **Different branch, dirty** — the new case. Drift stashes on the branch you are
  leaving and pops on that same branch when it returns, so the narrow claim above still
  holds.

The second case **asks first**. Being blocked by unrelated dirt is the friction `u`
exists to remove, so it is not a refusal — but being stashed without having agreed to it
is a surprise, and one `y`/`n` is the whole of the difference. A clean tree gets no
prompt and neither does a dirty tree on the branch you are already standing on: there is
nothing to warn about in either.

### The stash prompt

An overlay drawn in the panel's place, the same mechanism as the declare overlay and the
target picker, and bound like the delete confirmation — `y`/`enter` proceed, `n`/`esc`
decline. It **names the plan in run order** rather than asking "are you sure?": which
branch is being left, that the work is stashed there, that Drift checks the branch out,
updates and publishes it, and that it comes back and puts the work down where it picked
it up. A prompt that said less would be the same surprise with an extra keystroke.

Asked at **step 0**, before the fetches, and that placement is the decision:

- The question is about the user's own uncommitted work, which is fully known then.
  Nothing a fetch returns could change the answer.
- A verb whose promise is one keypress must not stop for input *in the middle*. Press
  `u`, press `y`, walk away — the alternative is a sequence that pauses after a network
  round trip, which is the babysitting this exists to remove.
- The cost is accepted: a sequence that prompts and then finds nothing to do. It ends by
  saying nothing was touched, which is true.

Declining is the screen's ordinary cancel — the sequence is still on its read-only head,
so nothing is stashed, nothing is merged, and there is nothing to undo. Accepting
resumes at step 1 and nowhere else: **the prompt gates whether the sequence runs, never
how**, so there is exactly one path through the mutating steps.

## Pulling the target

Drift compares against `origin/<target>` and never checks a *target* out — that half of
the invariant is untouched — so "pull the target main" means: **update that
remote-tracking ref, then merge it.** That is what `git pull` is; Drift just does the two
halves against a ref it never has to visit.

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

## Pulling the branch's own upstream (`u`)

The same split, applied to the branch instead of the target: fetch `origin/<branch>`,
then merge it. Normally a fast-forward, and often a no-op.

It exists because without it `u` is wrong on a second machine: it would merge the target
into a *stale* branch and then be unable to push the result — which is the exact failure
the verb was added to prevent. It is also what makes a push rejection rare enough to be
worth treating as a genuine handoff rather than a routine outcome.

A **conflict here is a real halt**, not a rollback of somebody else's doing: it means the
branch diverged from itself, and no amount of merging the target will settle it. Drift
rolls back and names the two refs to reconcile.

A branch with **no upstream** has nothing to pull. The step is skipped and said so — the
same rule as an unfetchable target — and the missing upstream surfaces properly at the
push.

## Pushing (`u`)

The push is what makes "this branch is up to date" a claim about the remote rather than
about one laptop, and it is the whole of the difference the dashboard has to render.

That is the **`⇡` glyph** on a branch row: commits this branch holds that
`origin/<branch>` does not. Without it the two verbs are indistinguishable on screen —
`↓behind ↑ahead` measures against the *target*, so a branch merged locally and one
merged and published render identically, and the difference between `s` and `u` would
live only in the help. `s` leaves `⇡`, `u` clears it. A branch with no upstream at all
renders `⊘`, the third answer the push already treats as distinct from zero. Read once
per sweep for the whole repo (`Upstreams`), and unpaired with the target: a branch can be
level with its target and still unpublished.

**Never forced.** A rejection means the branch moved on the remote after the fetch read
it, which is someone else's commit — exactly the class of thing Drift stops and hands
back rather than resolving. It is **not** a failure of the sequence: the branch is left
updated and merged locally, the user is still returned and their work still popped, and
the report says the publish is the one thing that did not happen. The same is true of a
branch with no upstream, which cannot be published at all.

The rejection is read from `git push --porcelain`'s flag column rather than from git's
message text — the same rule as everywhere else in the git layer — because a rejected
push and an unreachable remote are the same non-zero exit and need opposite responses.
The refspec is written out in full rather than left to `push.default`: a branch may track
an upstream under a different name, and publishing the right commits to the wrong ref is
the failure that would otherwise hide behind a bare push.

## Halts

Every halt is a **handoff**: Drift stops, names what it found, and leaves the
reconciliation to the human. That is the same rule as an unmergeable file, and it is
permanent.

### Held-file collision (step 3) — before anything is touched

The hazard [`local-only-changes.md`](./local-only-changes.md) names: something incoming
changed a file you hold on this machine. Drift does not rely on Git's behavior here (it
varies by version — abort vs. clobber), it checks first:

```
git diff --name-only <branch>...<ref>    # what each merge will bring in
∩  held set (skip-worktree `S`-tagged + fenced exclude block)
```

**Every** ref about to be merged is checked, not just the target's: for `u` the branch's
own upstream can carry a change to a held file exactly as readily.

Non-empty → halt, list the colliding paths with their notes, and stop. Nothing has been
stashed or merged. The user releases the hold, or reconciles by hand, and re-runs.

Empty (the common case) → the held set rides the rest of the sequence untouched, with no
re-apply step, because a plain `git stash` cannot see it. **Step 4 must never grow a
`-u` or `-a` flag**; that is the one thing this sequence owes area 6, and adding either
would silently break the promise. The held set rides the *checkout* the same way: git
refusing to switch because a skip-worktree file differs between the branches is a result
worth halting on, which is why the checkout is never forced.

### Merge conflict (steps 6 and 7) — roll the whole thing back

A conflict here means both sides *committed* to the same file. Drift rolls back — abort
the merge, return to the branch the sequence started on, put the stash back — and reports
the conflicting paths, flagging which of them are unmergeable, since that is what
determines whether the reconciliation is a text merge or a trip to an external tool.

**The sequence is therefore atomic across its mutating steps: it either lands whole, or
it leaves no trace.** The user ends up byte-for-byte where they started, holding a list
of what they are about to have to deal with. Nothing is half-merged, and nothing is
sitting in a stash they have to remember about.

The rollback aborts a merge only when one is actually in flight, which it finds out by
asking rather than by being told. That is what lets every post-stash halt share one
rollback path, including the ones where a merge failed without conflicting and so left
nothing to abort — and it is safe precisely because the preconditions refused to start on
top of anybody else's operation.

If a step of the rollback itself fails, Drift **stops there** rather than running the
next one: a rollback already not going to plan is exactly when carrying on turns one
problem into two. That case outranks whatever prompted it in the report — the work is
still stashed and the user may be standing somewhere they did not choose — and the
commands that finish it by hand are named.

### Pop conflict (step 10) — halt in place, stash retained

Your uncommitted work vs. the target's newly-merged content. This is the case the whole
feature exists for: with the unmergeable file's local edit safely in the stash, the merge
itself was clean, and the conflict surfaces here — over a `-merge` file, with no conflict
markers ever written into it, which is precisely the flow `CONTEXT.md` describes.

**This halt does not restore, and the asymmetry with a merge conflict is deliberate.** `git stash
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
  still absent) means nothing was stashed, and the pop is skipped entirely.
- **`stash@{0}` is a position, not an identity.** Anything that stashes concurrently —
  another terminal, an IDE, a hook — shifts it. Drift records the stash **commit OID**
  created in step 4 and verifies `refs/stash` still points at it before popping. If it
  does not, Drift **refuses to pop**, names the OID it was looking for, and tells the user
  their work is in the stash list under Drift's message. Popping the wrong stash is not a
  recoverable mistake in the way most git mistakes are.

The stash is created with an identifying message — `drift: <verb> <branch> ← <targetKey>`
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

- **`s` and `u` on a branch row** run the sequence — named actions `shelve` and
  `update`. Both are offered (and their keys documented in the `?` overlay) on the
  dashboard, and the report screen names the verb that is running.
- **One at a time.** While a sequence is in flight, a second one is refused, not
  queued — either verb. Two concurrent stash/merge sequences against one index is not a
  state worth being able to reach.
- **The running sequence is visible step by step** — the user must be able to see which
  of fetch / check / stash / switch / merge / push / return / pop is happening. A
  sequence that mutates the working tree behind a single undifferentiated spinner gives
  the user nothing to reason about when it stops. The step list is the one for the verb
  that is running, so `s` never shows steps it will not take.
- **A step with nothing to do says so**, rather than sitting unticked forever. "Pending"
  and "not needed" are the two things a stopped sequence must never conflate — and the
  answer comes from what git reported, never from a prediction made a step earlier.
- **Cancellation applies to the fetches only.** `esc` kills an in-flight fetch, as it already
  does on the dashboard. Once the stash is taken the sequence runs to a halt or to
  completion; there is no cancelling into an undefined middle.
- **Quitting mid-sequence is allowed, deliberately.** `q` and `ctrl+c` are not trapped
  while the mutating steps run: an escape hatch you can be locked out of is worse than
  the state it protects. And that state is already handled — a half-finished merge is
  what the `OperationInProgress` precondition exists to detect, so the next run refuses
  and names it rather than stacking on top. The failure mode is self-healing, which is
  exactly why it does not justify trapping the user. `u` widens what can be left behind —
  a quit mid-sequence can leave you on a branch you did not choose, with your work in a
  stash — so the report names the way back on every path Drift itself takes.
- **Every halt names its next action** — the git command that resolves it, in the same
  register as area 6's staged-change refusal (`git restore --staged`). Drift's job is to
  hand back a well-lit problem, not a stack trace.
- A held-file collision may reuse the **area-5 diff panel** to show what the target did
  to each colliding path. Natural, and the panel already exists — but not required for
  the area to be done.

Neither `s` nor `u` collides with any bound key on the dashboard.

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

Area 17 added three more:

- `Checkout(ctx, branch)` — `git switch --quiet <branch>`, deliberately unforced.
- `Upstream(ctx, branch)` — the remote-tracking ref a branch follows, `""` when it
  follows none, read via `for-each-ref` so "no upstream" is empty output and exit 0
  rather than an exit code to be told apart from a real failure.
- `Push(ctx, remote, local, remoteBranch)` — `git push --porcelain`, distinguishing
  updated / already-up-to-date / **rejected**.
- `Upstreams(ctx)` — the same question as `Upstream`, asked of every local branch at
  once, for the dashboard's sweep. One shell-out for the whole repo rather than one per
  row. Present-with-an-empty-value and absent are **different answers**: the first is a
  branch that has never been published (`⊘`), the second is a branch that is not there.

Every one of these takes a `context.Context` and runs through the existing `run` helper,
so the editor-proof environment above is set in **one** place rather than per call site.
`Push` is the single exception, and only in one respect: it needs stdout kept on a
non-zero exit, because that is where the rejection is reported.

## Out of scope

- **Batch update.** One branch per keypress, either verb. "Update every paired branch"
  is where this is heading and is deliberately not here: one conflict mid-sweep raises
  questions about the other branches that the per-branch path does not have to answer
  yet, and ADR 0002 should cover one reversal rather than two.
- **Force-pushing, ever.** A rejected push is someone else's commit, and it is handed
  back. There is no flag for this and there is not going to be one.
- **Reconciling anything.** Drift surfaces conflicts and hands to the human — the
  permanent rule from `CONTEXT.md`, and the reason the sequence has halts at all.
- **Committing the resolution**, staging resolved files, or `merge --continue`. Once
  Drift has handed back, the repo is the user's again.
- **Rebase.** `CONTEXT.md` pins merge; a rebase replays your commits *through* the
  unmergeable file repeatedly, which is the failure mode shelving exists to avoid.
- **`git stash -u` / `-a`.** Would sweep up exactly the local-only changes area 6 holds.
- **Stash management** — listing, dropping, or applying stashes Drift did not create.
  Drift creates one stash and pops the same one; `git` owns the rest.
- **Pushing, for `s`.** That verb brings the target *in* and never sends anything out;
  it is `u` that publishes, and the split is the whole distinction between them.
- **Setting an upstream.** A branch that has never been published is reported, with
  `git push -u` named. Choosing the remote and the remote name on the user's behalf is a
  guess, and publishing something new is the wrong place to make one.
