---
id: 016
title: The lead page shows its tree, and a round shows its specs
kind: feature
size: M
judge: opus
status: proposed
lens: ux
proposed_by: f3d2851e-4460-4cd8-87b7-359407c1c774
proposed_on: 2026-10-15
---

## Opportunity
A round is drawn on the map "like a lead with executors" (README, Proposal rounds), and its page is the lead page. For `buildermesh-round-1` that page's main column is a card reading `spec` / `no spec`, because a round owns no spec, and nothing on the page says the round has two engineers or what state they are in; their names appear only inside timeline rows.

![round page, desktop](assets/016-round-desktop.png)

On the phone the empty spec card and the empty inbox card take the first screen, and the timeline, the one card with content, starts at the bottom.

![round page, phone](assets/016-round-phone.png)

The same shape hurts a lead with executors: its page shows the spec but not the executors, their states, branches or unread counts, which `bmesh executor list` prints on the CLI and the map draws in the tree.

## Proposal
In:
- A `tree` card at the top of the main column: the node's children, one row each with badge, short name, branch and `N unread`, the way `bmesh executor list` prints them; absent when the node has no children. Rows link to the child's page (`#/lead/<child path>`, which the API already serves for any node).
- For a `round` node, the spec card becomes `specs`: the ledger rows whose `proposed_by` is one of the round's threads, read from the same `origin/main` the lead page reads a spec from, as `NNN title · status` lines, each opening the spec's body in place. Before any land, the card reads `no specs landed yet` instead of `no spec`.
- Empty cards (`no messages`, no spec) drop to a single muted line and sort after cards with content on the phone layout.

Out: editing or reviewing specs from the page (that is `bmesh spec review` and the chat); showing executors' worktrees from other hosts.

Judge: opus, because the API shape and the ledger read are pinned by the README and the work is wiring, not taste.

## What you'll see
Open a round's page during a round: the two engineers with their states and unread counts, then the specs as they land, then inbox and timeline. Open a lead's page during a build: its executors, then its spec.

## Verification
The verify commands; `web_test.go` cases for the tree card and the round's `specs` card against a fixture ledger; README's lead-page paragraph describes both cards.

## Risks and the alternative considered
Alternatives: (a) a separate round page: a third page type to keep in step with the lead page; (b) put the children in the map only: the map is already the place that shows them, but the push's click lands on the lead page, not the map. The tree card keeps one page type.

The case that users don't feel it: an operator who reads the round's pitch from the push and never opens its page. The page is the push's click-through target (`map_url` is `<map_url>#/lead/<path>`), so it is the first thing opened after a push.

## Needs from the operator
