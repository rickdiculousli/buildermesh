# Proposals

Half-page specs for this repo, one per file: `NNN-slug.md`, `NNN` a three-digit ID numbered in order. Screenshots and mocks they reference live in `assets/` as `NNN-<surface>.png`. Engineer and UI/UX engineer threads write them in proposal rounds (`bmesh ideate <repo>`); the operator reviews them (`bmesh choose-ideas <repo> "<reply>"`), and accepted ones are built by dispatched leads.

Each spec's frontmatter is its content's record: its status, who proposed it and what was decided. What is in flight lives on the mesh, not in git: a lead building a spec is its node (`bmesh mesh-status`), and a landing's proof is on the commit that landed, as its `Spec: NNN` and `Verified: <entries>` trailers. Specs landed before carry the fields their landing wrote (`built_by`, `branch`, `host`, `merged`, `verified`, `operator_steps`), and still read as they did.

## Lifecycle

- `proposed`: landed on `main` by the proposing thread, waiting for the operator's review.
- `accepted`, `adjusting` (being edited to match the operator's note, then `accepted`), `later`, or `rejected` with a one-line `reason`: the review. Rejected specs stay so they aren't proposed again.
- Built: an accepted spec stays `accepted` on `main` while its lead builds it (`bmesh build` records nothing there); a lead that can't finish reports it with `bmesh blocked`, and its node says so.
- `done`: set by the lead on its own branch, so the spec lands with its code; the landed commit's trailers carry the verified line. An older spec may read `building` or `blocked`, which nothing writes any more.

The template and field guide is `bmesh spec template`; `bmesh spec list --repo .` reads the ledger.
