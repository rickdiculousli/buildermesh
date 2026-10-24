---
id: 093
title: A hold says it waits for the operator and how to approve it, on every box
kind: ux
size: M
judge: opus
status: proposed
lens: eng
proposed_by: operator
proposed_on: 2026-10-24
---

## Opportunity

On 2026-10-24 the Mac held its self-level of buildermesh, 62 commits, so its sessions kept the old skills after v0.4.8. The Mac is secure through its own `[hosts] secure = true`; its roster row isn't. An earlier hold of the same repo had expired unapproved the day before.

The operator heard about it once, from a push whose title and body are both `ricks-macbook holds: buildermesh +62 commits (self-level)`. That line says neither that it waits on him, nor what it holds back, nor what to run. In his words: "the ntfy message doesn't tell me squat on what to do when something is held, or that even i should be doing anything at all".

After that push nothing shows the hold:
- not the session line;
- not `bmesh mesh-status`;
- nothing on any other box, though the operator's sessions mostly run on another box (k16).

Every refusal now names its next step (step 5 of `docs/design/2026-10-24-lean.md`). Holds, and the pushes the operator has to act on, are the surfaces still left.

## Proposal

In:

- **The hold's push** says it waits, what it holds back and the step.
  - A level. Title: `<host>: <repo> waits for your approval (+<N> commits)`. The body says first what the move would change:
    - a plugin's checkout: `New sessions on <host> load its skills and plugins.`;
    - the `[update] repo`: `<host>'s bmesh builds from it.`

    Then: `In a terminal on <host>: bmesh hosts approve <id> (it shows the commits first), or bmesh hosts deny <id> to leave it as it is. Expires <day hh:mm>.`
  - A registry proposal held on the admin box gets the same: `<host>: a registry proposal from <box> waits for your approval (<N> row changes)`, with approve showing the diff first.
- **Presence carries the box's holds:** for each, the repo (or `registry`), the commits, the id and the expiry, with expired ones dropped. With that:
  - The session line names them. On the holding box: `; held for your approval: buildermesh +62 commits, rickdiculousli-tools +1 commit (bmesh hosts held)`. On another box, in that box's entry: `ricks-macbook (awake, admin, 2 held)`.
  - `bmesh mesh-status` lists each hold as waiting on the operator, one line each, with the box and the command to run there.
  - The work skill's Status relays them as waiting on the operator, saying where to run the approve.
- **Every other push the daemon sends** is read for the same gap. One that needs the operator says what and how in its body. One that needs nothing doesn't read as if it does. The report lists what changed.

Out:
- what gets held;
- how approval works (the key, the Keychain dialog, the terminal rule);
- how a box's own `bmesh update` moves the skills it loads, which is a later step.

Judge: opus, because the operator acts on these words, and sessions relay them.

## What you'll see

- **On the phone:**
  - title: `ricks-macbook: buildermesh waits for your approval (+62 commits)`;
  - body: `New sessions on ricks-macbook load its skills and plugins. In a terminal on ricks-macbook: bmesh hosts approve eZ7q2nMtwSUHSEdc5n5dkg (it shows the commits first), or bmesh hosts deny eZ7q2nMtwSUHSEdc5n5dkg to leave it as it is. Expires Sat 23:47.`
- **In sessions:** a session on k16 opens with `on the mesh: ricks-macbook (awake, admin, 2 held)`. "What's waiting on me" answers with each hold and the command to run on the Mac.

## Verification

- **The repo's verify.**
- **Tests through the daemon and the commands:**
  - A held level's event, and the push built from it, carry the title, body, id and expiry. The same for a registry hold.
  - A box's presence lists its holds and drops an expired one.
  - The session line and `mesh-status` show the holds, for this box and for another.
- **On the real thing:** with a hold in a scratch state dir, the built binary's `bmesh hosts --here` and `bmesh mesh-status` show it as above.
- **The skill change** is measured with `scripts/skilltest` against the skills before it.

## Risks and the alternative considered

- **Presence gains a field.** A box on an older release ignores it and shows nothing of others' holds, which is today's behaviour.
- **The hold id in a push** is no secret: approving still needs that box's own terminal and key.
- **Alternative considered:** repeat the push until the hold is approved. That's noise on the phone. The session line and `mesh-status` reach the operator through the sessions he already uses.

## Upgrading

nothing

## Needs from the operator
