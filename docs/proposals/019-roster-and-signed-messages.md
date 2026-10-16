---
id: 019
title: Boxes join the mesh: an admin's roster, one-time invites and signed messages
kind: feature
size: L
judge: opus
status: proposed
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
---

## Opportunity
Boxes can act on each other only where SSH happens to reach. command-center's `repo_sync.py` and `prune_worktrees.py` drive every box over `ssh <host> bash -lc`, and `bmesh ideate` reaches another host the same way. In a fleet where the Mac reaches k16 but k16 can't reach the Mac, everything has to be driven from the Mac ("run it from the Mac; k16 can't SSH to the Mac", command-center AGENTS.md). Every box already shares the hub (the Mac is a NATS leaf of k16's), but nothing on the hub says which boxes belong to the fleet or proves who sent a message. Anyone holding the hub token can publish anything. This spec gives the mesh that membership and that proof. 020 builds the operations on top of it.

## Proposal
In:
- **Opt-in.** `bmesh init --hosts` (also offered at the end of init's multi-box branch) is the only way in. A single box never asks.
  - It asks: join (the code from an admin's invite), or be the fleet's admin (offered only when the hub has no roster yet); whether this box is **secure** (020: agent requests wait for the operator's approval); and which operations it accepts (020).
  - It writes only the `[hosts]` table (`enabled`, `admin`, `secure`, `accept`) into `config.toml`, keeping the file as it was as `config.toml.prev`. It never touches `repos.toml`.
  - Run again, it re-asks with the current answers as defaults. `init --force` keeps its meaning (start over).
  - A box joining a remote hub also gets `[runner] checkouts = ""`, so its daemon answers the mesh and runs no routines.
- **Keys and the state dir.**
  - The state dir is `$XDG_STATE_HOME/bmesh` (`~/.local/state/bmesh`) on Linux and `~/Library/Application Support/bmesh/state` on macOS, never the cache, which macOS purges.
  - Every joined box has an Ed25519 key in `<config dir>/host.key` (mode 0600).
  - **The admin key never sits in a file an agent can use.**
    - On macOS it goes in the login Keychain, through `/usr/bin/security add-generic-password -s bmesh-admin -a <host> -T ""`. Because the item trusts no app, macOS shows a dialog on every read, and only a person at the Mac can allow it. Headless agents can't, so they can't sign. The README says to choose "Allow", never "Always Allow".
    - On another OS it is a file, `<config dir>/admin.key`, sealed with a passphrase (scrypt, then XChaCha20-Poly1305; both are in golang.org/x/crypto, already a dependency).
    - `[hosts] admin_store = "keychain" | "file"`, defaulting to `keychain` on macOS.
    - Every admin command refuses without a terminal.
- **The roster** is the canonical list of who belongs. It lives in KV `bm-roster`, key `roster`, which is outside `bm.>`, so no stream overlaps.
  - The document is `{version, admin_key, hosts: [{name, key, secure, accept}], issued_at}`, signed by the admin key.
  - A box pins the admin key when it joins and keeps its last good roster in `<state dir>/roster.json`. It accepts a new roster only when the pinned admin key signed it and its `version` is higher. A deleted key or an old copy put back is ignored.
  - Each daemon loads the roster before it starts taking jobs, and again on every reconnect. It watches the key, and a KV watch hands over the latest value first, so a waking Mac gets the current roster before any job.
  - The admin box being asleep costs nothing: the roster is read from the hub, never from the admin.
- **Presence** is separate and only informational. KV `bm-hosts`, `<host>` holds `{rev, os, repos, notes}`, written by each daemon every minute. `bmesh hosts [--json]` prints the roster joined with presence: `asleep` when the entry's server timestamp is over 3 minutes old, and `secure` and `admin` marks.
- **The one-time invite.** It works only while the admin is at the terminal, so it uses core NATS request/reply and nothing is stored.
  1. On the admin box, `bmesh hosts invite <name> [--secure] [--accept ...] [--replace]` prints `<id>-<secret>`. The id is 8 hex characters and not secret; the secret is 128 random bits in base32. The invite is good for 10 minutes, and the command waits for the join.
  2. On the new box, `init --hosts --invite <code>` sends `{name, key, ts, salt}` on `bmhosts.join.<id>`, MACed with HMAC-SHA256. The MAC key is `HKDF-SHA256(secret, salt=id, info="bmesh join request v1")`.
  3. The admin burns the invite on the first request, whether it verifies or not. It refuses a name already in the roster unless the invite said `--replace`. It then publishes roster `version+1` and replies `{roster, admin_key}` under the same MAC, with `info="bmesh join reply v1"`.
  4. The new box checks the MAC, pins the admin key and stores the roster.
- **Revoking** is the admin's alone. `bmesh hosts revoke <name>` publishes the next roster without that box, so no other box can revoke anything. Rotating the admin key is out; a new admin means joining again.
- **The transport** is inbox messages in `bm-mesh`, the way routine jobs travel. A job for a box goes to `bm.inbox.host.<host>`, and that box's durable consumer `inbox-host--<host>` resumes where it stopped, so a box that was asleep gets what it missed, in order. A reply goes to `bm.hostreply.<caller>.<job id>`, readable later with `bmesh hosts replies`. `host` and `hostreply` join the reserved first tokens: the map, `watch` and the activity reader skip them.
- **Signed messages.** Every job, reply and approval is signed.
  - The signed bytes are a type tag (`bmesh/job/v1`, `bmesh/reply/v1`, `bmesh/approve/v1`, `bmesh/roster/v1`), then each field in a fixed order. Every field is a 4-byte big-endian length followed by its bytes. Strings are UTF-8, integers are decimal ASCII, booleans are `0` or `1`, and a list is its count followed by its items. `ts` and `expires` are RFC 3339 UTC to the second, and `salt` is 16 random bytes in hex.
  - A job's fields are `type, id, by, to, op, repo, args, ts, expires, salt`. A reply's are `type, job_id, job_salt, by, to, ok, text, ts, salt`.
  - `sig` is Ed25519 over SHA-256 of those bytes. Ed25519 is deterministic (RFC 8032), so one message gives one signature on every box.
- **A receiving box checks, in this order, and stops at the first failure:**
  1. `by` is in the roster. An op that needs the admin (020) needs the admin's key here.
  2. The signature verifies against that key.
  3. `to` is this box.
  4. `ts` is no more than 5 minutes ahead of this box's clock. A message from the future is nak'd and retried a minute later, until it expires, because a Mac just woken can be behind.
  5. `expires` hasn't passed, and `expires - ts` is at most 24h.
  6. The salt is not in `<state dir>/salts` under another stream sequence. The same sequence is a redelivery after a crash or sleep and goes ahead. A salt is kept until its message's expiry, and the store survives a restart.

  A failure is never run. It is answered `refused: <check>` and published as `bm.daemon.<host>.refused`. A daemon with ntfy pushes it, at most once per sender and check every 10 minutes. The caller checks a reply the same way, the reply naming its job's id and salt.

Out: per-box NATS users that would let the hub itself cut a revoked box off (a later spec); encrypting message bodies; rotating the admin key; approving from the phone; a Keychain item gated by Touch ID (it needs a signed app with entitlements).

Judge: opus, because the encoding, the order of checks, the invite, the Keychain store and the roster's monotonic version are a security interface every later operation rests on.

## What you'll see
On the Mac, `bmesh init --hosts` makes it the admin and a secure box, and stores the admin key in the Keychain. `bmesh hosts invite k16` raises the Keychain dialog, then prints a code. On k16, `bmesh init --hosts --invite <code>` prints `joined: roster v2, admin ricks-macbook`. `bmesh hosts` lists both boxes, with the Mac `asleep` when its lid is closed. A message changed in flight, replayed, sent to the wrong box, from a revoked box or more than a day old is refused, and the refusal reaches the phone.

## Verification
The repo's verify. New tests:
- golden vectors: a fixed key, message, `ts` and `salt` give fixed bytes and a fixed signature;
- one refusal test per check, a replay after a daemon restart included, and a redelivery of the same sequence passing;
- the roster: a lower version, a non-admin signature and a deleted key all leave a box's roster as it was;
- the invite: reused, expired and wrong codes (a wrong one burns it), a name already taken, `--replace`;
- `init --hosts` on a hand-written k16-style `config.toml` changes only `[hosts]` and `[runner] checkouts`;
- the admin commands refuse without a TTY;
- the Keychain store against a fake `security` (a new `BMESH_SECURITY_COMMAND`); the file store's seal and open;
- the reserved tokens don't reach the map or `watch`.

The real Keychain dialog is checked by hand on the Mac.

## Risks and the alternative considered
Threat model: whoever holds the hub token can read every message, delete keys and streams, and flood subjects. Signatures don't stop that. They stop such a party, or any box that isn't in the roster, from getting a box to act. Each box's `host.key` is readable by the agents on that box, so a compromised box can act as itself until the admin revokes it. 020 limits what that box can do. The admin key is the one secret no agent can use.

Alternatives:
- A shared fleet secret: one leaked box could forge any box.
- Boxes vouching for each other: revocation has to cascade and can race.
- Per-box NATS users: stronger, since the server drops a revoked box, but it means hub config and reloads on every change. It is the later hardening, not a replacement.

## Needs from the operator
On the Mac: run `bmesh init --hosts` as admin and secure, then `bmesh hosts invite k16`, and run the join on k16 (over `ssh k16`).
