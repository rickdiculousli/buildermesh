---
name: mesh-status
description: Use when the operator asks "what's building", "mesh status", "where is X", "how is X doing", "what's waiting on me", "anything asking", "stop X" or "wipe X" - X a repo, a spec or a node. Reads bmesh mesh-status (and bmesh spec list for a named repo) and summarises it as working, asking, silent and done.
---

# Mesh status

The mesh is the record; read it and summarise, never guess from the runner.

1. **The mesh.** `bmesh mesh-status --json` lists every node with its path, role, state, since, runner and unread count. For a named repo, `bmesh spec list --repo <repo>` shows the ledger (`NNN status size kind lens title`); for a named spec, its lead is node `<repo>-<NNN>`, and `bmesh thread-status <path>` shows that one node with its last event text.
2. **The summary**, by state, leads and rounds first with their executors and children nested, routine instances left out unless asked:
   - **working**: path, since when, and the spec or round it is on;
   - **asking**: the question in the operator's words (`bmesh thread-status <path>`), the thing most worth answering first;
   - **silent** and **blocked**: path, since when, the block's reason; a block whose reason starts `stopped by` or `stopped in` is a node the operator stopped: list it as **stopped**, waiting on no one (`bmesh node forget <path>` clears it, `bmesh --node operator send <path> "<what to do>"` resumes it);
   - **held**: path, the gate and why, and how long ago it was held (each row's `hold` in the JSON): a coordinator's hold, not waiting on the operator unless the operator holds it; a hold a day old or more is named, since it may be forgotten (`bmesh unhold <path>` releases it);
   - **landing**: path, how long since it ran `bmesh ready`, and what its daemon is doing (after its table `bmesh mesh-status` prints a `<path> landing 6m: review job … running on k16` line per request; each row's `ready` in the JSON): a lead whose box's daemon is landing its change, waiting on no one. One whose request is old (an hour or more) is named, since that box's daemon may be down;
   - **done since the last look**: path and what landed (the commit in the `done` text). "The last look" is the last status this session gave; the first look lists what finished today.
   A node with unread messages is noted (the count) so the operator knows a reply is waiting on it.
   - **steps for the operator**: `bmesh spec steps` prints a line per landed spec whose steps for the operator aren't all ticked (`operator: <repo> NNN <title>: X of Y open (bmesh spec steps NNN --repo <repo>)`, the lines plain `mesh-status` ends with); name each spec and its count, and the command that lists and ticks them.
3. **Keep it short.** One line per node, the asks first; a quiet mesh is one line.
4. **This box's bmesh.** When `bmesh update --notice` prints a line (this box's binary is behind the bmesh release, or the code on main, that it follows; its daemon's update waits on `bmesh update`; or upgrade notes are pending), pass each on as the last lines of the summary, word for word.

When the operator says "stop X" (a lead, a round or a thread), that is `bmesh --node operator node stop <path> --text "<why>"`, never a free-text STOP message: a message is mail, and mail wakes a thread. When the operator says "wipe X" (remove it "as if it never ran"), that is `bmesh --node operator node stop <path> --text "<why>"`, then `bmesh node forget <path> --wipe` (`--dry-run` first shows what goes): it removes X and every node under it, with their events, inboxes, key history, briefs, worktrees and branches; never the `nats` CLI or the mesh token.

End with one line: what is running, and what waits on the operator (the asks and blocks, by path, and the specs with open steps). Say what each thing in it is in plain words (a spec or issue by what it does, its number after it in brackets if at all): the line is final and reads on its own, with nothing the operator has to look up.
