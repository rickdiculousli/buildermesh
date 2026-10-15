# Proposals

Half-page specs for this repo, one per file: `NNN-slug.md`, `NNN` a three-digit ID numbered in order. Screenshots and mocks they reference live in `assets/` as `NNN-<surface>.png`. Engineer and UI/UX engineer threads write them in proposal rounds started from command-center; Rick reviews them, and accepted ones are built by dispatched threads.

Each spec's frontmatter is the ledger: its status, who proposed and built it, the branch, the host and the merge commit. There is no other record.

## Lifecycle

- `proposed`: landed on `main` by the proposing thread, waiting for Rick's review.
- `accepted`, `adjusting` (being edited to match Rick's note, then `accepted`), `later`, or `rejected` with a one-line `reason`: Rick's review. Rejected specs stay so they aren't proposed again.
- `building`: dispatched; `built_by`, `branch` and `host` name the build thread.
- `done` with the `merged` commit, or `blocked` with a one-line `reason`: set by the build thread.

The template and field guide is `command-center/templates/proposal.md` (`uv run ~/repos/command-center/tools/proposals.py template`); `proposals.py list --repo .` reads the ledger.
