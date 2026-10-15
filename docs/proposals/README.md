# Proposals

Half-page specs for this repo, one per file: `NNN-slug.md`, `NNN` a three-digit ID numbered in order. Screenshots and mocks they reference live in `assets/` as `NNN-<surface>.png`. Engineer and UI/UX engineer threads write them in proposal rounds (`bmesh propose <repo>`); the operator reviews them (`bmesh spec review <repo> "<reply>"`), and accepted ones are built by dispatched leads.

Each spec's frontmatter is the ledger: its status, who proposed and built it, the branch, the host and the merge commit. There is no other record.

## Lifecycle

- `proposed`: landed on `main` by the proposing thread, waiting for the operator's review.
- `accepted`, `adjusting` (being edited to match the operator's note, then `accepted`), `later`, or `rejected` with a one-line `reason`: the review. Rejected specs stay so they aren't proposed again.
- `building`: dispatched; `built_by`, `branch` and `host` name the build thread.
- `done` with the `merged` commit and the `verified` line, or `blocked` with a one-line `reason`: set by the build thread.

The template and field guide is `bmesh spec template`; `bmesh spec list --repo .` reads the ledger.
