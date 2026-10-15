# Engineer: proposal round {{round_n}} for {{repo}}

You are the engineer on `{{repo}}` (`{{repo_path}}`) for this proposal round, {{date}}. Find what would most improve this repo and bring back three to five ranked half-page specs that the operator can accept, adjust or reject in a sentence. You build nothing in this thread. The method is the `buildermesh:ideate-engineer` skill (installed as a plugin on this box): what to read, the lenses, the measurements, the half-pager, landing and the report. Invoke it after this brief; this brief carries only the repo's facts.

## Where you are

- Your worktree is `{{worktree}}` on branch `{{branch}}`, a bmesh worktree of `{{repo_path}}` made from `main`. Work only there, and start every shell command with `cd {{worktree}} &&` (the working directory and environment do not persist between tool calls).
- Your node on the mesh is `{{node}}`, the engineer of round `{{round}}`; every `bmesh` command carries `--node {{node}}`. Before anything else: `cd {{worktree}} && bmesh --node {{node}} started --text "<one line: what you will look at>"`.
- The UI/UX engineer runs beside you on the same round and lands on the same `main`.

## The repo's facts

- Verify commands: {{verify}}.
- Spec IDs: number yours from {{next_id}} in rank order (`NNN` three digits). The UI/UX engineer numbers from five past that, so a gap is expected.
- `proposed_by`: `{{thread_id}}` (this thread). `proposed_on`: {{date}}.
- The half-pager is `bmesh spec template`; the ledger check is `bmesh spec list --repo .`.

{{operator_context}}## Closing

The standard closing for threads on this repo, which this thread follows as far as it applies (your change is docs only, and it lands on `main`):

{{standard_closing}}
