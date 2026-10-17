---
id: 064
title: Before a protected key is used, bmesh shows a dialog saying exactly what it will sign, so the operator never answers a bare Keychain prompt
kind: ux
size: M
judge: opus
status: accepted
lens: ux
proposed_by: dfdc9673-27b2-4f3f-9f17-95a5b436de66
proposed_on: 2026-10-17
---

## Opportunity
Every use of the admin or approve key on macOS brings up the system's Keychain prompt. macOS writes its text, and it names only the tool and the item (`security` wants to use the "bmesh" item). bmesh reads the key with `security find-generic-password` (`internal/hosts/keystore.go`), and the only context is one terminal line before it, `Keychain dialog follows for: <action>`. When the prompt pops up over another window, the operator approves without seeing even that line.

On 2026-10-17 the operator answered, one after another, prompts for: two roster changes (`hosts set ... --accept`), a `nix` sent to two boxes, a registry publish adding a `code` key, and two more roster changes from an upgrade-notes script. Each prompt looked the same. They said: "the dialogs need to specify details, currently it tells me nothing on what it is trying to do."

It matters beyond comfort. A prompt with no details trains the operator to approve any Keychain prompt about bmesh, and anything on the Mac that runs `security` against that item gets the same prompt. The signed-releases spec (063) adds a publish key whose whole point is that its dialog means "this ships to every install".

## Proposal
In:
- **One request, shown before the key is read.** `Store.Get` takes a request instead of an action string. A request has:
  - a title (`Sign roster v6`, `Publish registry v3`, `Approve level of rickdiculousli-tools`, `Send nix to 2 boxes, then publish registry v2`, `Sign release buildermesh v0.3.1`);
  - its details, at most 12 lines, naming exactly what changes:
    - a roster change as its diff, `k16: accept nix,ping -> nix,ping,stop-thread`;
    - a registry publish as its diff (`buildermesh: code: (none) -> 14 paths`);
    - an approve as the repo, `<head8> -> <target8>`, the commit count and the first log lines;
    - a release as the tag, the commit, the specs and the notes summary;
    - a nix as the repo and the boxes;
  - the SHA-256 of the exact bytes it will sign, shown as its first 8 hex.

  The bytes are composed before the dialog and signed as they are. Nothing is composed again after the operator answers.
- **macOS: a bmesh dialog first.** It's a native dialog (`osascript` `display dialog`, a caution icon) titled `bmesh: <title>`, showing the details and `signs <sha8>`, with buttons `Cancel` (the default) and `Continue to Keychain`.
  - `Cancel`, or no answer within 2 minutes, refuses with exit 3: `cancelled at the dialog: nothing signed`.
  - `Continue` brings up the Keychain prompt as today.
  - The terminal prints the same title and details, so a log of the session shows what was approved.
- **The file store** (Linux, Windows, or `[hosts] key_store = "file"`) prints the title and details before asking for the passphrase.
- **One request per batch.** A command that signs several things with one read, such as `repos remove` (each box's `nix`, then the registry), lists them all in the one dialog.
- **The rule, in the README's Hosts and Security sections and the agents snippet:** a Keychain prompt about bmesh that isn't preceded by a `bmesh:` dialog naming what it signs is not bmesh's. Deny it.
- The publish key from 063 uses the same request, and so does any key added later.

Out:
- Changing the Keychain prompt's own text, which macOS fixes.
- Touch ID with a custom reason (LocalAuthentication's `localizedReason`, in one dialog). It needs a code-signed binary with a keychain entitlement, which a `go install` build isn't.
- A dialog on Linux desktops.

Judge: opus, because the dialog must show the bytes actually signed: a summary composed separately from them would be a dialog that lies.

## What you'll see
`bmesh hosts set k16 --accept nix,ping,stop-thread` at the Mac first shows the dialog:
- **title:** `bmesh: Sign roster v6`;
- **body:** `k16: accept nix,ping -> nix,ping,stop-thread` and `signs 3fa9c21e`;
- **buttons:** `Cancel`, `Continue to Keychain`.

Then the Keychain prompt appears. `Cancel` leaves the roster at v5.

## Verification
The repo's verify, plus:
- `internal/hosts` keystore tests with a fake `osascript` and `security` in `testdata/`:
  - the dialog runs before `security`;
  - `Cancel` or the timeout never reaches `security` and signs nothing;
  - the sha shown is that of the bytes signed;
  - the details are capped at 12 lines.
- One test per call site (`hosts set`, `hosts approve`, `repos publish`, `repos remove`, `init --hosts`) that the request names the change: the roster diff, the registry diff, the approve log, the nix boxes.
- **Live, by the lead on the Mac:** one `hosts set` that changes nothing visible (re-setting a row's current accept list) shows the dialog, and `Cancel` leaves the roster version unchanged.

## Risks and the alternative considered
- **Something else on the Mac can show a fake `bmesh:` dialog before its own Keychain prompt.** The dialog informs; it isn't the guard, and the Keychain prompt still is. What it adds is that a prompt without one stands out, and the operator is told to deny it.
- **Dialog fatigue moves to the new dialog.** Details that name the change, rather than a fixed sentence, are what keep it read. A batch shows one dialog, not one per item.
- **Alternative: print more in the terminal only.** The terminal is often not where the operator is looking when the prompt appears, which is the reported problem.

## Upgrading
nothing

## Needs from the operator
