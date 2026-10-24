# buildermesh, after lean: a coordinator that keeps itself, and skills that match the binary

2026-10-24. The operator decided this after the six steps of [lean](2026-10-24-lean.md) landed in v0.4.8, asking what would make work flow easier and better. Its specs are 093, 094 and 095; the rest follows them.

## Why

- **A coordinating session can't see its own context.** In lean the coordinator ran past 400k before compacting, by hand, at the operator's prompt.
  - Its own units went to Agent-tool subagents and were landed by hand. That spent the coordinator's context, and the work stayed invisible on the mesh until it was done.
- **A box's skills and its binary drift both ways.** Sessions load the plugin from the operator's checkout of this repo; the binary comes from a release.
  - k16's skills ran ahead of its binary: they called `bmesh build` forms v0.4.7 refused.
  - The Mac's skills lagged behind: its secure self-level held the checkout 62 commits back, and nothing said so but one push.
- **A hold didn't say it waited on anyone.** Its push read `ricks-macbook holds: buildermesh +62 commits (self-level)`, with no step. The operator didn't know he had anything to do.

## Decisions

1. **Holds name their step, on every box (093).**
   - The push says the hold waits for the operator's approval, what it holds back, the command and when it expires.
   - Presence carries each box's holds, so every box's session line and `mesh-status` show them.
   - Every push the operator must act on says how.
2. **bmesh tells a session its context (094).** It's portable: it comes from bmesh itself, not one harness's hook.
   - **The line:** any `bmesh` command in a session's main thread ends with a line when the context has crossed 250k, then each further 50k.
   - **The advice:** from 300k, compact at the next milestone; from 400k, at the next safe point; never while exploring or investigating; right before executing a finished design is a milestone.
   - **Who hears it:**
     - The advice is a default the session judges. A session about to finish finishes instead. A step that needs what the session holds is finished first.
     - The coordinator compacts. A lead hands off by its skill and retires. Executors and subagents never hear it, since a subagent's commands carry its parent's session.
   - **Compacting is two queued messages**, `/compact` naming the state file, then a resume. When the operator asked for a pause, it's the `/compact` alone. bmesh reminds a session that just compacted to read its state file.
   - **Saved context:**
     - the plan and its decisions go in `docs/design/`;
     - what's building is the mesh's, and what landed is git's;
     - the state file holds the rest, somewhere that outlives the session, never a temp dir.
   - Claude Code is read first. Codex gives a command no session id yet.
3. **The coordinator's units are specs built by leads.** A design that splits into units is a series of specs, each built with `bmesh build` and landed by its lead through `ready`. The coordinator watches the end lines; Agent-tool subagents are for read-only investigation. 093 to 095 are the first built this way, and the work skill says so once they have.
4. **The binary carries its plugin (095).**
   - The skills, hooks and manifest are embedded and written to bmesh's own plugin dir by the binary that runs. So whatever updates the binary updates the skills.
   - Each box switches once, through the release's upgrade notes.
   - The operator's decision: the skills go live when he runs `bmesh update`, on a secure box too. The approval gate stays for repos levelled from git.
5. **One batch of small fixes** after 095:
   - the README art's renderer finds Linux Chromium;
   - the skill test's fake prints `build`'s batch lines;
   - the review routine may run `git show`;
   - the daemon test `TestEachBoxCapsItsOwnConcurrency` is flaky: find the cause;
   - the other ship repos are rescaffolded for v0.4.8's text.
6. **Habits, not builds:**
   - Each skill-text change is measured with the skill test, two runs a case, with a case added for every real miss.
   - Quota joins the context line later.

## Order

093 and 094 together; then 095, kept apart to hold less in flight on a tight week; then the fixes. Each lands on its own, with verify, a review sized to it, and the skill test for skill text.
