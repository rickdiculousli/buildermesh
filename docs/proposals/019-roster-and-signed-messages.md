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
Boxes can act on each other only where SSH happens to reach. command-center's `repo_sync.py` and `prune_worktrees.py` drive every box over `ssh <host> bash -lc`, and `bmesh ideate` does the same (cmd_ideate.go:383). In a fleet where the Mac reaches k16 but k16 can't reach the Mac, everything has to be driven from the Mac. Every box already shares the hub (the Mac is a NATS leaf of k16's), but nothing on the hub says which boxes belong to the fleet or proves who sent a message. Anyone holding the hub token can publish anything. This spec gives the mesh that membership and that proof, and one op (`ping`) to show it works end to end. 020 and 023 build the real ops on top.

## Proposal
In:

**Opt-in.** Nothing here runs unless `[hosts] enabled = true`, and only `bmesh init --hosts` sets it. A single box never asks.
- `init --hosts` asks:
  - join (with an invite) or be the admin;
  - whether this box is **secure** (020 and 023 hold what it is asked to do until the operator approves);
  - whether its daemon runs routines (default no on a secure box, yes otherwise).
- It writes the `[hosts]` table (`enabled`, `admin`, `secure`) and, when routines are off, `[runner] checkouts = ""`. Nothing else in `config.toml` changes, the old file is kept as `config.toml.prev`, and `repos.toml` is never touched.
- Run again, it re-asks with the current answers (from `config.toml`, `host.key` and `<state dir>/roster.json`) as the defaults. `init --force` keeps its meaning (start over).
- **Accept lists are the roster's**, set by the admin (`hosts invite --accept`, `hosts set`), not by the box.

**The state dir** is `$XDG_STATE_HOME/bmesh` (`~/.local/state/bmesh`) on Linux and `~/Library/Application Support/bmesh/state` on macOS, never the cache, which macOS purges.

**Keys.**
- **Host key.** Every joined box has an Ed25519 host key in `<config dir>/host.key` (mode 0600). The agents on that box can read it; it proves only "this box".
- **Protected key.** The admin box and every secure box also have one more Ed25519 key that agents can't use: the admin key (signs rosters, registries and `nix`) or the approval key (signs approvals).
- **On macOS it lives in the login Keychain.**
  - It is written through `security -i` with the key on stdin, so it never appears in `ps`, as an item with an empty trusted-app list (`-T ""`). Every read then raises a macOS dialog that asks for the login password.
  - Before each read, bmesh prints `Keychain dialog follows for: <action>`. The README says to deny any dialog that wasn't announced, and to choose "Allow", never "Always Allow".
  - Before reading, bmesh checks the item's ACL and refuses (`the admin key's Keychain item trusts <app>; remove it in Keychain Access`) if any app has been added, which is what one "Always Allow" does.
  - Over SSH there is no dialog and the read fails, so admin and approve commands run at the Mac's own screen.
- **On another OS** it is `<config dir>/admin.key` (or `approve.key`), sealed with a passphrase (scrypt, then XChaCha20-Poly1305, from golang.org/x/crypto, already a dependency).
- `[hosts] key_store = "keychain" | "file"`, defaulting to `keychain` on macOS.
- The terminal check on these commands is a convenience. The dialog and the passphrase are the control.

**The roster** is the canonical list of who belongs. It lives in KV `bm-roster`, key `roster`, which is outside `bm.>`, so no stream overlaps.
- **The document.** `{version, admin_key, hosts: [{name, key, secure, approve_key, accept}], issued_at}`, signed by the admin key.
- **Accepting a roster.** A box pins the admin key when it joins and keeps its newest valid roster in `<state dir>/roster.json`. It accepts a roster only when the pinned key signed it and its `version` is higher.
- **Writing a roster.** The admin takes the next version from its own `roster.json`, never from the hub, and writes with compare-and-set on the KV revision.
- **Keeping the roster alive.** Every daemon re-puts its newest valid roster each minute when the hub's copy is missing, unsigned or lower. A roster deleted or rolled back by a token holder comes back within a minute from any awake box.
- **Reading a roster.** A daemon reads the roster key (`kv.Get`) before it checks each job. A job carries the sender's `roster_version`; a receiver holding a lower one naks it for a minute until it has caught up. A hub it can't reach naks too.
- **Admin commands.**
  - `bmesh hosts set <name> [--secure] [--accept ...]` changes a row.
  - `bmesh hosts revoke <name>` publishes the next roster without it. No other box can revoke.
  - The admin's fingerprint (the first 16 hex characters of its key's SHA-256) is printed at join and by `bmesh hosts`.
- **Bootstrap.**
  - `init --hosts --admin` is always allowed. When a roster already exists it warns `a roster signed by <fingerprint> exists; becoming admin replaces it on every box you invite again`.
  - A fresh invite re-pins a box that already pinned an admin. A token holder who creates the first roster costs the operator one re-invite per box, and nothing more.

**Presence.** Each daemon writes KV `bm-hosts`, key `<host>`, each minute: `{host, rev, os, routines, repos: [{name, path, origin}], notes, ts}`, signed with its host key.
- `bmesh hosts [--json]` prints the roster joined with presence:
  - `asleep` when the entry's server timestamp is over 3 minutes old;
  - `unverified` (with notes withheld) when the signature fails;
  - the `secure` and `admin` marks.
- Presence is never the source of a path or a decision (023 resolves paths from the receiver's own registry).

**The one-time invite.** It works only while the admin is at the screen, so it uses core NATS request/reply and nothing is stored.
1. On the admin box, `bmesh hosts invite <name> [--secure] [--accept ...] [--replace]` prints `<id>-<secret>`. The id is 8 hex characters and not secret; the secret is 128 random bits in base32. The invite is good for 10 minutes, and the command waits.
2. On the new box, `init --hosts` prompts for the code (never argv, so it isn't in `ps` or history) and sends `{name, key, approve_key, ts, salt}` on `bmhosts.join.<id>`, MACed with HMAC-SHA256. The MAC key is `HKDF-SHA256(secret, salt=id, info="bmesh join request v1")`.
3. The admin burns the invite on the first request whose MAC verifies. A request that fails the MAC is dropped without burning, and three failures burn it. The name must equal the invite's name and be a valid path token. A name already in the roster is refused unless the invite said `--replace`.
4. The admin publishes the next roster and replies `{roster, admin_key}` under the same MAC, with `info="bmesh join reply v1"`.
5. The new box takes the first reply whose MAC verifies, ignoring junk, pins the admin key and stores the roster.

**The transport** is inbox messages in `bm-mesh`, the way routine jobs travel.
- **Jobs.** A job for a box goes to `bm.inbox.host.<host>`. That box's durable consumer, `inbox-host--<host>` (AckWait 10m, `InProgress()` every minute while a job runs, MaxAckPending 1), resumes where it stopped, so a box that was asleep gets what it missed, in order.
- **Replies.** A reply goes to `bm.hostreply.<caller>.<job id>`, and `bmesh hosts replies` reads them later.
- **Reserved first tokens.** `host` and `hostreply` join them. One helper in `internal/mesh`, `IsMeshInternal(subject)`, replaces the separate lists in names.go:21, the activity reader (activity.go:61), the push loop's `nodeEvent` (push.go), `status`'s `splitSubject` (cmd_status.go), `watch` and the map's tree (map.js `apply`), so all of them skip the same tokens.

**Signed messages.** Every job, reply, approval, roster, registry (023) and presence entry is signed.
- **The signed bytes** are a type tag followed by every field in a fixed order:
  - Type tags: `bmesh/job/v1`, `reply/v1`, `approve/v1`, `roster/v1`, `registry/v1`, `presence/v1`.
  - A scalar is a 4-byte big-endian length, then its bytes. A string is UTF-8, an integer decimal ASCII, a boolean `0` or `1`.
  - A list is a 4-byte big-endian count, then each item. A struct is its fields in their listed order.
  - Lists of names (`accept`, `repos`, `hosts`) are sorted and deduplicated before encoding.
  - `ts` and `expires` are RFC 3339 UTC to the second. `salt` is 16 random bytes in hex.
- **The fields.**
  - Job: `type, id, by, to, op, repo, args, roster_version, ts, expires, salt`.
  - Reply: `type, job_id, job_salt, by, to, ok, text, ts, salt`.
  - Approve: `type, job_id, job_salt, job_sha256, by, decision, ts, salt`.
- **The signature.** `sig` is Ed25519 over SHA-256 of those bytes. Ed25519 is deterministic (RFC 8032), so one message gives one signature on every box, and the tests pin both.

**A receiving box checks, in this order, and stops at the first failure:**
1. `by` is in the roster. An op that needs the admin needs the admin's key here.
2. The signature verifies.
3. `to` is this box.
4. `roster_version` is not ahead of this box's roster (otherwise nak for a minute).
5. `ts` is no more than 5 minutes ahead of this box's clock (otherwise nak for a minute; a Mac just woken can be behind).
6. `expires` hasn't passed, and `expires - ts` is at most 24h.
7. The op is in this box's `accept` in the roster.
8. The salt's record in `<state dir>/salts`:
   - none: write `started` and run;
   - `done`: ack and send the stored reply again, never re-run;
   - `started` (a redelivery after a crash or sleep): resume only an idempotent op (`ping`, `level`, `prune`), and answer any other `interrupted`.

A record is kept until its job's expiry, and the store survives a restart.
- **Failures.** A failure is answered `refused: <check>` and never run.
  - **Expired** (check 6) is acked, answered and logged only, so a weekend asleep doesn't flood the phone.
  - **Roster, signature, `to` and replay failures** are published as `bm.daemon.<host>.refused`. Any daemon with `[notify]` pushes it once, through the existing `push-<seq>` claim, at most once per sender and check every 10 minutes.
- **Replies.** The caller checks a reply the same way: it must be signed by the job's `to` and name the job's id and salt.

**`ping`** is this spec's one op. It answers with the box's revision and roster version. `bmesh hosts ping [<host>]` prints the answer from each box, or `asleep: queued until <expires>`.

**Waking.** A daemon tells it has woken from a gap of 3 minutes or more between its minute ticks, measured on the wall clock (`time.Now().Round(0)`), since monotonic time stops during sleep. On wake it re-reads the roster before it takes a job.

**The version hint** compares with this host's own daemon's newest `started` first, and with the hub's newest only when this host runs no daemon. A Mac daemon that doesn't self-update then never tells k16's leads to run `bmesh update`. cmd_version.go:48, as 021 left it.

Out:
- per-box NATS users that would let the hub itself drop a revoked box (a later spec);
- encrypting message bodies;
- rotating the admin key;
- approving from the phone;
- a Keychain item gated by Touch ID (it needs a signed app with entitlements).

Judge: opus, because the encoding, the order of checks, the invite, the key store and the roster's version rules are a security interface every later op rests on.

## What you'll see
On the Mac, `bmesh init --hosts --admin` makes it the admin and a secure box, and stores the admin key in the Keychain. `bmesh hosts invite k16 --accept ping,level,prune` announces the Keychain dialog, then prints the code. On k16, `bmesh init --hosts` asks for it and prints `joined: roster v2, admin ricks-macbook a1b2c3d4e5f60718`. `bmesh hosts` lists both boxes, and `bmesh hosts ping` answers from k16 and shows the Mac `asleep` when its lid is closed. A message changed in flight, replayed, sent to the wrong box, from a revoked box or more than a day old is refused, and the refusal reaches the phone.

## Verification
The repo's verify. New tests:
- golden vectors for every type tag: a fixed key and message give fixed bytes and a fixed signature;
- one refusal test per check;
- the salt record's three states, across a daemon restart;
- the roster: a lower version, a non-admin signature and a deleted key leave a box's roster as it was, and an awake daemon re-puts a deleted roster;
- the invite: reused, expired and wrong codes, three failures, a junk reply before the real one, a taken name, `--replace`;
- `init --hosts` on a hand-written k16-style `config.toml` changes only `[hosts]` and `[runner] checkouts`;
- the key store against a fake `security` (a new `BMESH_SECURITY_COMMAND`): a widened ACL is refused, and the file store seals and opens;
- the reserved tokens don't reach the map, `watch` or the activity reader;
- a wake detected across a faked wall-clock jump.

The real Keychain dialog is checked by hand on the Mac.

## Risks and the alternative considered
Threat model:
- Whoever holds the hub token can read every message, delete keys and streams, and flood subjects. Signatures don't stop that. They stop such a party, or any box not in the roster, from getting a box to act.
- Revocation is best-effort against a token holder, who can delay a box's view of the newest roster but can't forge one. Per-box NATS users close that later.
- A box's `host.key` is readable by its own agents, so a compromised box can act as itself until revoked. 020 and 023 limit what that box can do.
- The admin and approval keys are the secrets no agent can use.

Alternatives:
- A shared fleet secret: one leaked box could forge any box.
- Boxes vouching for each other: revocation has to cascade and can race.
- Per-box NATS users: stronger, but they need hub config and reloads on every change. They are the later hardening, not a replacement.

## Needs from the operator
On the Mac:
- install its daemon (`bmesh init --service`, then `launchctl bootstrap gui/$(id -u) <plist>`);
- run `bmesh init --hosts --admin` and answer secure;
- run `bmesh hosts invite k16`;
- run the join on k16 (over `ssh k16`, typing the code at its prompt).
