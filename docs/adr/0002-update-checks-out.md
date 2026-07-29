# ADR 0002 — The update sequence checks branches out

**Status:** Accepted · 2026-07-28 · supersedes the "Drift never checks anything out"
invariant in `CONTEXT.md` and the *Scope* section of `docs/specs/shelve-sequence.md`

## Context

`CONTEXT.md` has carried a standing invariant since the project brief: **Drift never
checks anything out.** The shelve spec restated it as scope — `s` runs on the branch
you are on, and any other branch's row names `git switch` instead — and gave the
reason, which was never squeamishness:

> A stash belongs to the branch it was taken on. A cross-branch sequence would have
> to `checkout` → merge → `checkout` back → pop, and every arrangement of those steps
> either carries uncommitted work across a branch boundary or pops it onto the wrong
> tree.

The spec deferred auto-checkout rather than refusing it forever, and named the two
questions it would have to answer first: what happens to uncommitted work on the
branch you are leaving, and whether Drift wants to own the "put me back where I was"
bookkeeping.

Dogfooding v0.2.0 turned the invariant into the sharpest kind of finding: **the tool
does not do the thing it was built to do.** Drift's whole premise is that one ticket
fans out into one branch per target main, and the payoff for storing that grouping is
one keypress per branch. But going down the branch list pressing `s` refuses on every
row but the one you happen to be standing on, so the payoff only ever arrives for a
single branch. The user is sent back to `git switch` — the tool Drift exists to sit
above — between every row.

Two further gaps came with it, and neither is a design question: the sequence never
pulls the branch's *own* upstream (so on a second machine it merges the target into a
stale branch and produces something that cannot be pushed), and it never pushes (so
"this branch is up to date" is a claim about one laptop).

## Decision

**The update sequence checks branches out, and owns the return.** A new verb `u`
runs, in order:

1. Preconditions — nothing in progress, a paired target that resolves
2. Fetch the target ref **and** the branch's own upstream
3. Recompute the divergence; check the local-only held set against everything incoming
4. Stash, if the tree is dirty
5. Check the branch out, if it is not already current
6. Merge the branch's own upstream
7. Merge the target
8. Push
9. Return to the branch the user was standing on, and pop

The invariant's *reason* is preserved rather than discarded. A stash still belongs to
the branch it was taken on: Drift stashes on the branch it is leaving and pops on that
same branch once it has come back. Uncommitted work never crosses a branch boundary.
What changes is that the guarantee is now enforced by the **return** instead of by
never leaving — which is precisely the bookkeeping the spec named as the price of
admission, and the bulk of the work in the area.

`s` is kept, unchanged. The two verbs differ by commitment: `s` merges the target into
the checked-out branch and publishes nothing, leaving the branch ahead of its own
remote; `u` finishes the job on any paired branch.

## Consequences

- **Every halt path unwinds.** A conflict at the upstream merge, at the target merge,
  or a failure at the push rolls the merge back, returns to the starting branch, and
  pops — in that order, stopping at the first failure rather than stacking the next
  step on a rollback that is already not going to plan. There is no halt that leaves
  the user standing somewhere they did not choose, except the one where the return
  itself failed, which is reported outright with the two commands that finish it.
- **The read-only prefix survives, and is why both fetches were hoisted above the
  stash.** Area 7's central mechanic is that every check able to refuse the sequence
  runs before anything mutates, so a refusal has nothing to undo. `u` merges two refs
  rather than one, and both can be brought up to date without touching the working
  tree — so the divergence recompute and the held-set check still happen before the
  stash, exactly as they did for `s`.
- **The one refusal that survives is the return itself.** If Drift cannot get back to
  the starting branch, it does **not** pop: popping wherever it happens to be standing
  is the single thing this whole arrangement exists to prevent.
- **`internal/git`'s package doc changes.** "Nothing checks anything out" was true of
  every call in the package; now `Checkout` exists, alongside `Push` and `Upstream`.
  Checkout is deliberately unforced — git refusing to switch is a result the sequence
  needs, since something the stash could not see (a skip-worktree file that differs
  between the branches) is exactly what area 6 exists to protect.
- **Never force-push.** A rejected push means the branch moved on the remote after
  step 2 read it, which is someone else's commit — the class of thing Drift stops and
  hands back. The branch is left updated and merged locally; only the publish did not
  happen, and the report says so.
- **The dashboard owes a new signal.** `↓behind ↑ahead` measures against the *target*,
  so a pushed branch and a locally-merged-but-unpushed one render identically. Without
  an ahead-of-`origin/<branch>` column the screen is silent about the only step that
  touches the remote, and the difference between the two verbs is legible only in the
  help. That column is roadmap 17b.

## Alternatives rejected

- **Keep the invariant; make the user switch.** The status quo, and the thing
  dogfooding rejected. It is not a small friction — it removes the tool's headline
  payoff for every branch but one, on exactly the multi-branch repo shape Drift was
  built for.
- **Cross-branch merge without a checkout.** Git can merge into a ref it has not
  checked out only in the fast-forward case, and the interesting case here is
  precisely the one that is not. Anything more (a temporary worktree, plumbing-level
  tree merges) is a great deal of machinery to avoid a checkout whose only real hazard
  is the stash, which the return already answers.
- **`u` replaces `s`.** Rejected: a verb that has shipped is a verb someone has, and
  the local-only path is worth keeping reachable for the case where you want to see
  the merge before it goes anywhere. The cost is real and accepted — two near-identical
  sequences are two entries in a help table generated per action, so their one-line
  descriptions have to carry the distinction on their own.
- **Batch update across every paired branch.** Deliberately not here. One conflict
  mid-sweep raises questions about the other branches that the per-branch path does not
  have to answer yet, and this ADR should cover one reversal rather than two.

## Notes

One case is built but gated: leaving a branch that has **uncommitted work** on it. The
machinery handles it — the stash is taken on the branch being left and popped on that
same branch when Drift returns — but being stashed without having agreed to it is a
surprise, so it is refused for now, at the read-only stage where a refusal costs
nothing. The confirmation overlay that names the plan and unlocks it is roadmap 17b.
