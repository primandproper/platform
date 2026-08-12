<!--
================================================================================
HOW TO WRITE AN ISSUE IN THIS REPO — read this before filling in the scaffold.
Delete this whole comment before submitting.
================================================================================

platform-go is a library. Its issues are not feature requests; they are arguments
about where a responsibility belongs. A good one makes a reader who has never seen
the code agree, from the issue alone, that the current split is wrong.

The three sections below (Problem / Proposal / Scope) are the whole structure.
What separates a good issue from a filed-and-ignored one is what goes in them.


--------------------------------------------------------------------------------
TITLE: name the package, then the specific tension — not the feature
--------------------------------------------------------------------------------

Format: `package/path: the thing that is wrong`, package lowercase, no ticket
prefixes, no "Add"/"Support"/"Implement".

The title should be falsifiable. A reader should be able to disagree with it.

  BAD   Add gRPC error mapping for search
  GOOD  search/text: the index's own refusals have no transport mapping, so every
        consumer invents one

  BAD   Improve test container setup
  GOOD  testutils/containers/pgtest: per-test schema isolation, and the migration
        lock key that makes it parallel

  BAD   WebAuthn support
  GOOD  authentication/webauthn: the missing factor, and the ceremony store that
        has to outlive one replica

If the title could describe any library's version of this feature, it is too vague.


--------------------------------------------------------------------------------
## Problem — what platform does today, and what that forces on consumers
--------------------------------------------------------------------------------

Open with what platform currently owns and what it leaves out. The gap is the
subject, not the feature.

Then do these four things. They are what make the issue persuasive:

1. CITE A REAL CONSUMER, WITH file:line.
   Point at code that exists in a repo that consumes this one. "A consumer might
   need this" is speculation; "a consumer already wrote it at
   internal/store/postgres/pgtest/pgtest.go:98" is evidence. Verify the lines
   exist before filing; a stale citation makes the whole issue unreadable six
   months later.

   Quote the consumer's own comments when they record what the omission cost.
   Those comments were written by someone who had just been burned, and they are
   better evidence than anything written afterwards.

2. NAME THE FAILURE MODE CONCRETELY.
   Not "this is error-prone" — say what breaks, when, and why nobody notices.
   The best sentences in this tracker are of this shape:

     "an env var whose name is one underscore off is not an error at any layer:
      it is not read, the JSON value stands, and the service comes up looking
      healthy with the wrong configuration"

     "the authorization code is issued by one instance and redeemed at another,
      so the login fails whenever the load balancer does its job"

     "reporting the page size as the total is what told clients that a truncated
      page was the entire result set"

   Silent, intermittent, and only-under-load failures are the ones worth filing.
   Say which kind it is.

3. COUNT IT.
   "11 call sites", "60 of 73 files hand-roll the same Archive", "171 constants
   spread over two dozen files". A number turns a matter of taste into a matter
   of fact, and it sizes the work for whoever picks it up.

4. SAY WHAT IS ACTUALLY GENERIC.
   If you are proposing a move, state plainly which part is platform's and which
   stays with the consumer. "Zero application imports today" is the strongest
   possible version of this claim — check it (`go list` over the package's
   imports) rather than asserting it.


--------------------------------------------------------------------------------
## Proposal — a concrete API, not a direction
--------------------------------------------------------------------------------

Write the Go signatures. A proposal that cannot be typed out is not ready to file.

  func Hydrated[E, T any](
      ctx context.Context,
      index textsearch.IndexSearcher[T],
      query string,
      filter *filtering.QueryFilter,
      idOf func(*T) string,
      hydrate func(ctx context.Context, ids []string) ([]*E, error),
  ) (*filtering.QueryFilteredResult[E], error)

Number the parts if there is more than one, so they can be argued with or landed
separately.

THEN — and this is the highest-value paragraph in most issues — list THE DETAILS A
REIMPLEMENTATION GETS WRONG. Every one of these is a bug someone already paid for:

  - `sql.ErrNoRows` from the fetch is an expected outcome, not a failure
  - nothing may hold a session on the template, or `CREATE DATABASE ... TEMPLATE`
    refuses to run
  - the suffix to collect is `EventType`, not `ServiceEventType`, or a dozen
    identity events fall outside the catalog and fail the dispatch gate

If you know one of these and leave it out, the port ships without it and the bug
is rediscovered in the new home.


--------------------------------------------------------------------------------
## Scope — what is deliberately excluded, and why
--------------------------------------------------------------------------------

State what is NOT in this issue. Uncontained issues do not get picked up.

Good exclusions have a reason attached:

  "Postgres only. `dialect` exists and the generators already branch on it, but
   there is one implemented backend and inventing the second one speculatively is
   how the abstraction comes out wrong."

  "The repository side stays with the consumer: `FetchFunc` is the repository's
   existing get-by-ID. Platform supplies neither, and shouldn't."

Link related issues here and say HOW they relate — "#101 made index events
deliverable, this makes them unforgettable" — not a bare number.


--------------------------------------------------------------------------------
WHEN IT IS A DECISION, NOT A TASK
--------------------------------------------------------------------------------

Some issues are policy questions (does platform ship generated protobuf? does a
package take on a new external dependency?). File those as questions: replace
Proposal with the options, in order of increasing commitment, each with its real
cost. Say which you would pick and why. Add the `question` label.

Do the same when the proposal has a genuine cost — a new dependency, a versioning
promise, generated code in the tree. Naming the cost yourself is what makes the
recommendation credible.


--------------------------------------------------------------------------------
DO NOT
--------------------------------------------------------------------------------

- Do not use acceptance-criteria checkboxes. They restate the proposal as a to-do
  list and go stale the moment the design shifts.
- Do not write user stories. There is no user; there is a consuming repo.
- Do not pad with severity/priority theater. The failure mode already conveys it.
- Do not file "improve X" or "refactor Y" with no named defect.
- Do not cite code you have not read. Every file:line here is checkable.
- Do not describe what the code does where the issue needs to say what it costs.

BEFORE FILING: search the tracker. If an open issue covers the same package from a
different angle, comment on it with your evidence instead of opening a rival —
a second independent data point on an existing issue is worth more than a
duplicate. (See #210, which collected two different isolation strategies that way.)

LABELS: `enhancement` for gaps and new seams. `bug` for something that is wrong
today. `breaking-change` when it alters the exported API — that requires a /vN
module bump in the same change. `question` for policy decisions. `documentation`
when the fix is prose.

================================================================================
-->

## Problem

_What platform owns today, what it leaves to consumers, and what that costs them._
_Cite a consuming repo with `file:line`. Name the failure mode concretely. Count the duplication._

## Proposal

_The Go signatures you are asking for, numbered if there is more than one part._
_Then: the details a from-scratch reimplementation would get wrong._

## Scope

_What is deliberately excluded, and why. Related issues, and how they relate._
