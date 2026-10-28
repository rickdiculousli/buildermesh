# Starting leads

- `bmesh build <repo> <NNN>...` on the row's `host` (elsewhere over `ssh -o BatchMode=yes <host> "bash -lc '<command>'"`; an unreachable host is said, and nothing starts).
- Their words beyond the pick go in `--context -` through a quoted heredoc.
- A model named for one spec is that spec's own `build --model <m> --family <F>`, since `--model` on a batch overrides every judge.
- Specs that edit the same files go one after another in one family.
- Each `warning:` is reported as printed.
- A spec you write for a lead lands first (`bmesh spec next-id`, `bmesh spec template`, `status: proposed`, `Propose NNN: <title>`), written as what can be checked, and you name its file before the lead starts, while it's cheap to change.

Then do what `build`'s `next:` lines say: watch, relay asks in their words, and drain the family's feedback when `watch-done` names it. Their words for a lead while it runs: [in-flight.md](in-flight.md). A batch ends, once done and drained, with a release of each repo it landed in that is released by tag: [release.md](release.md).
