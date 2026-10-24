# Proposals

Half-page specs for this repo, one per file: `NNN-slug.md`, `NNN` a three-digit ID numbered in order. Screenshots and mocks they reference live in `assets/` as `NNN-<surface>.png`. Proposal rounds write them (`bmesh ideate <repo>`: one thread, or two when the app runs on another box), a spec for each idea sized M or larger; the operator picks the ones to build (`bmesh build <repo> <NNN>...`), each built by a dispatched lead, and declines or shelves the rest (`bmesh spec set --id <NNN> --status rejected|later --land`).

Each spec's frontmatter is its content's record: its status, who proposed it and what was decided. What is in flight lives on the mesh, not in git: a lead building a spec is its node (`bmesh mesh-status`), and a landing's proof is on the commit that landed, as its `Spec: NNN` and `Verified: <entries>` trailers. Specs landed before carry the fields their landing wrote (`built_by`, `branch`, `host`, `merged`, `verified`, `operator_steps`), and still read as they did.

## Lifecycle

- `proposed`: landed on `main` by the proposing thread, waiting for the operator.
- Picked: the operator builds it with `bmesh build`, which is the accept. It still reads `proposed` on `main` while its lead builds it (`bmesh build` records nothing there); a lead that can't finish reports it with `bmesh blocked`, and its node says so.
- Then one of:
  - `done`: set by the lead on its own branch, so the spec lands with its code; the landed commit's trailers carry the verified line.
  - `rejected` with a one-line `reason`: declined. Rejected specs stay so they aren't proposed again, and `bmesh build` refuses them.
  - `later`: shelved; it can still be picked.

Older specs may read `accepted`, `adjusting`, `building` or `blocked`, which the operator's choices no longer write (the fixer still writes `accepted` on a fix it rules); they read as before, and any of them can be picked.

The template and field guide is `bmesh spec template`; `bmesh spec list --repo .` reads the ledger.
