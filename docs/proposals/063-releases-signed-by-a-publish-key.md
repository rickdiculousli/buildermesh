---
id: 063
title: A bmesh release installs only when bmesh's publish key signed it, on every box that runs bmesh, so a push from an agent's box can't reach anyone's
kind: feature
size: L
judge: opus
status: proposed
lens: eng
proposed_by: dfdc9673-27b2-4f3f-9f17-95a5b436de66
proposed_on: 2026-10-17
---

## Opportunity
Every box builds and runs whatever bmesh's repo says, and nothing checks who wrote it:
- **The daemon updates itself** to each new release tag (`[update] track = "release"`).
- **`bmesh update` builds** the newest tag, or `main` under `--latest`.
- **The upgrade notes are read from `main`**, not the tag. `bmesh update --apply --yes` runs their shell scripts on the box.
- **Since 058 and 059**, bmesh's own fix lead cuts releases with no human step.

Anything that can push to the repo or create a tag can therefore run code on every box, the admin box included. On 2026-10-17 that is every agent on k16, which pushes to GitHub as the operator. One of those leads reading content a third party wrote (an upstream fork, a web page) is enough. A prompt injection there becomes a commit, then a tag, and then it runs on the Mac at the operator's next `bmesh update`.

The admin box already holds a key that no agent can use. It sits behind the Keychain dialog (or the file store's passphrase), and it signs the roster and approves held levels, and every box pins its fingerprint. Releases have no such key, and they go unsigned. That guard is the model for one.

On the GitHub side, 2026-10-17 hardened what it could: rulesets on `main` and `v*` tags (no force-push, move or delete), Actions off, and a fine-grained token on k16 with no admin rights. None of that stops a push or a new tag from an agent's box.

## Proposal
**Two roles, two keys.** The *publish key* signs bmesh's releases; a fleet's *admin key* signs its roster. They are separate on purpose:
- the admin key's dialog comes up for every roster change, approval and registry publish, so answering it is a habit, while the publish key's comes up only when a release ships to every install;
- re-making a fleet's admin (a lost box, a new admin box) must not stop every bmesh install from updating, and a new publish key must not touch a roster;
- a key used once per release can be kept more strictly than an everyday one.

**The publish key** is made once, on the box that releases, with `bmesh release key new`. It's an Ed25519 key stored like the admin key (macOS Keychain behind its own item and dialog; elsewhere the file store with its own passphrase), never readable by an agent. `bmesh release key show` prints its public half, and `bmesh release key backup <file>` writes it encrypted under a passphrase asked at the terminal, for the operator to keep off the box. Releases are checked against the publish key, never against a box's roster:
- **The publish key's public half is compiled into the binary** (`internal/selfupdate.Publisher`, a list, so a rotation can overlap). Every box checks a release against the key in the binary it runs: this fleet's, another fleet's, or a single box with no roster.
- **A rotation is a release.** A release signed by a key the running binary trusts may carry a new key list, and the next binary trusts that. A release that only changes the key, signed by an unknown key, is passed over like any unsigned one.
- **The first install is trusted the way Go trusts a module.** `go install .../cmd/bmesh@vX.Y.Z` is checked against Go's checksum database (`sum.golang.org`), which proves the tag never changed but not who made it. The README says so, and says how to check the signature by hand (`bmesh release --verify <tag>`).
- **A fleet that follows its own fork** (`[update] upstream` naming another repo) names its publisher in its roster: `bmesh hosts set --publisher <hex>` on its admin box, signed with the roster. It never goes in a box's `config.toml`, which an agent on the box can edit.

The rest applies to the repo `[update]` follows, on every box:
- **A release is a tag the admin signed.**
  - `bmesh release <repo>` still works out the version, checks verify and writes nothing new.
  - On the box holding the publish key, at its own screen, it signs with that key, behind its own Keychain dialog. It creates an annotated tag whose message ends with a trailer, `bmesh-signed: <publisher fingerprint> <hex Ed25519 signature>`.
  - The signature covers, in a fixed order: the repo's name, the tag, the commit's full SHA, and the blob hash of `docs/upgrading/<tag>.toml` at that commit, or `none`.
  - On a box without the publish key, or with no terminal, `release` doesn't tag. It records the release as **proposed**: a `release` event on the mesh, `<repo> v0.3.1 at <sha8> waits on the publish key's signature (NNN, ...)`, pushed once through `[notify]` and shown on `mesh-status` and the map, with the command that signs it: `bmesh release <repo> --sign v0.3.1`.
  - The fix lead's release step and the `build` skill's step 7 propose; the operator signs.
- **`update` and the daemon install only signed releases.**
  - The newest release is the newest `v*` tag whose trailer verifies against a publisher key the running binary trusts (or the roster's `publisher`) and whose commit is the tag's.
  - An unsigned or badly signed tag is passed over, never installed. It is named once in the log and on `bmesh update`: `v0.3.1 is not signed by bmesh's publisher: passed over`.
  - A release that would come after a passed-over tag waits, and nothing newer is guessed.
- **The notes run are the notes signed.** The walk reads `docs/upgrading/<tag>.toml` at the signed commit, and the file's blob must match the signed hash.
  - A fix to a release's notes after it is tagged (the fixes on 2026-10-17 to v0.2.0's and v0.3.0's notes) is a signed notes amendment: `bmesh release <repo> --sign-notes v0.3.0`, on the box holding the publish key, signs the current file from `main`. It creates a tag `bmesh-notes/v0.3.0/<n>` with the same trailer over the same fields.
  - The walk takes the newest signed amendment over the release's own notes.
  - Unsigned notes on `main` are never run.
- **`--latest` stays possible, and it's never quiet.**
  - `bmesh update --latest` builds code nobody signed. It prints `unsigned: <rev8> is not a release the admin signed`.
  - At a terminal it asks `Install it anyway? [y/N]`, and without a terminal it is refused.
  - `track = "latest"` in the daemon is refused on a box in a roster, with `track = "latest" installs unsigned code: set it to release`. The exception is a box whose roster row says it runs agents anyway (the op `unsigned-ok` on its accept list, set by the admin, never by the box's own config), so the risk is chosen per box, on the admin box.
- **Squatting.** A `v*` tag the admin didn't sign can't be moved or deleted under the rulesets. `release --sign` then skips that version (`v0.3.1 exists unsigned: releasing v0.3.2`), so a squatted name never blocks a release.
- The README's Releases, Updates, Upgrading, Hosts and Security sections say it. `docs/agents-snippet.md` and the skills (`build` step 7, `fix`) say a release is proposed and signed by the operator.

Out:
- Signing commits.
- A transparency log of releases.
- Signing a ship repo's own releases, which have no installer.
- Reproducible builds.
- The plugin: the operator's plugin checkout is already levelled only after approval on the admin box.
- Keys other than the admin's.

Judge: opus, because it changes `release`, `update`, the daemon and the walk, which every box and the fix pipeline depend on, and a wrong check either installs unsigned code or never updates.

## What you'll see
- The fix lead finishes, and the phone shows `buildermesh v0.3.1 waits on your signature`.
- At the Mac, `bmesh release buildermesh --sign v0.3.1` shows the specs and the notes, brings up the Keychain dialog, and prints `released buildermesh v0.3.1 at <sha8> (signed)`.
- k16 updates within minutes.
- A tag pushed from k16 by hand is shown as `passed over` and never installed.

## Verification
The repo's verify, plus, over a bare origin with a roster:
- **`cmd_release_test.go`:**
  - `release` on a box without the publish key proposes and creates no tag;
  - `--sign` with the publish key creates the tag with a trailer that verifies, and the admin key's signature does not;
  - `release key new` refuses when a key exists, `show` prints the public half, and `backup` writes a file that only its passphrase opens;
  - an unsigned tag at the next version is skipped;
  - `--sign-notes` makes a verifying amendment tag.
- **`cmd_update_test.go` and the daemon's update tests:**
  - only a signed tag is installed;
  - an unsigned newer tag is passed over and named;
  - a tag signed by another key, or whose commit differs, is refused;
  - notes on `main` that differ from the signed blob aren't run, and a signed amendment is;
  - `--latest` asks at a terminal and is refused without one;
  - a box with no roster checks against the compiled-in key exactly as a box in one does;
  - a release that rotates the key, signed by the old one, makes the next binary trust the new one; a rotation signed by an unknown key is passed over;
  - a roster's `publisher` overrides the compiled-in key for a fork, and a `publisher` in `config.toml` is an unknown key.
- **Live, by the lead:** k16's daemon passes over an unsigned test tag (created and then left; it can't be deleted under the rulesets, so it uses a throwaway name such as `v0.0.0-signcheck`, which the release pattern excludes, against a test origin), then installs a release signed at the Mac.

## Risks and the alternative considered
- **The operator becomes the release bottleneck,** which is intended: a release reaches every box, so one dialog per release is the cost of keeping the fix pipeline automated everywhere else. A release waiting on a signature is pushed and shown until it's signed.
- **The publish key is lost.** No box anywhere can take a release until a binary with a new key is installed by hand (`go install ...@<tag>`), so it is backed up when it's made (`bmesh release key backup`). A rotation release, made while the old key still works, is the planned way.
- **Another fleet's operator can't tell a good release from a signed bad one.** Signing proves who released, not that the release is safe; that is still the publisher's review, and the notes the walk shows before `--apply`.
- **Alternative: git's own signed tags** (`gpg` or SSH signing, `git verify-tag`). They need a signing key that git can use without the Keychain dialog's guard, and an allowed-signers file on every box. bmesh's own key store already guards a key behind the Keychain dialog or a passphrase, and the binary is already what every box trusts.
- **Alternative: hold every self-update for approval,** like a secure box's level. That asks on every box for every release, while a signature asks once, on one box.

## Upgrading
- Every box installs only signed releases from this one on, whether or not it is in a roster. There is no per-box key to turn that off, since an agent on a box can edit that box's config. A box allowed unsigned code is marked in the roster, by the admin: `bmesh hosts set <box> --accept <its list>,unsigned-ok`.
- The first signed release is the admin's first `bmesh release --sign`. Releases from before it are unsigned, and a box already at one stays there until then.
- A box with `[update] track = "latest"` either gets `unsigned-ok` on its roster row or changes `track` to `release` (an ask, default `release`).

## Needs from the operator
After this lands, at the Mac: `bmesh release key new`, `bmesh release key backup <file>` kept off the Mac, then the lead's commit that compiles the public half in, and the first `bmesh release buildermesh --sign`. The publish key is the key every bmesh install trusts.
