---
name: status
description: Use when the operator asks "what's building", "mesh status", "where is X", "how is X doing", "what's waiting on me" or "anything asking" - X a repo or a spec. Reads bmesh status (and bmesh spec list for a named repo) and summarises it as working, asking, silent and done.
---

# Status

The mesh is the record; read it and summarise, never guess from the runner.

1. **The mesh.** `bmesh status --json` lists every node with its path, role, state, since, runner and unread count. For a named repo, `bmesh spec list --repo <repo>` shows the ledger (`NNN status size kind lens title`); for a named spec, its lead is node `<repo>-<NNN>`, and `bmesh state <path>` shows that one node with its last event text.
2. **The summary**, by state, leads and rounds first with their executors and children nested, routine instances left out unless asked:
   - **working**: path, since when, and the spec or round it is on;
   - **asking**: the question in the operator's words (`bmesh state <path>`), the thing most worth answering first;
   - **silent** and **blocked**: path, since when, the block's reason;
   - **done since the last look**: path and what landed (the commit in the `done` text). "The last look" is the last status this session gave; the first look lists what finished today.
   A node with unread messages is noted (the count) so the operator knows a reply is waiting on it.
3. **Keep it short.** One line per node, the asks first; a quiet mesh is one line.

End with one line: what is running, and what waits on the operator (the asks and blocks, by path).
