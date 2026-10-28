# Leads in flight, and what's waiting

## Their words for a lead

As `--node operator`:

- anything for it to hear: `bmesh --node operator send <path> "<their words>"`;
- "hold X" is `bmesh hold --gate now` ("hold land on X" without `--gate`; "land none until I say" is `build --hold land`);
- "release X" for a held lead is `unhold`;
- "stop X" is `node stop`, never a message: mail wakes a thread;
- "wipe X" is a stop, then `bmesh node forget <path> --wipe`, `--dry-run` first.

## What's building, what's waiting on me

`bmesh mesh-status`, asks first. A box's hold (`<box>: <repo> waits for your approval`) is the operator's: name it and the `bmesh hosts approve <id>` they run in a terminal on that box; never run it yourself. Then `bmesh spec steps`, then any `bmesh update --notice` lines word for word.
