base: {{base}}
spec: {{spec_path}}

Check the change against its spec too, beside what the prompt asks. The spec is `{{spec_path}}`; it is in the diff, set `done` by the change's lead. Read it whole: `git diff -U100000 {{base}}..HEAD -- {{spec_path}}` shows every line.

- Check each promise in `## What you'll see`, each `## Verification` item past the repo's verify, and each line of `## Shown` against the code.
- A promise the code doesn't keep is `Must fix`: its case is the spec's words and what the code does instead, with the file and lines.
- A `Shown` line the code couldn't produce (an output no path prints, a test that doesn't exist or doesn't check what it claims) is `Must fix`. Check its `Easy to miss:` line the same way.
- A promise only a run outside git can show (a real run, a soak, numbers from a measurement) is `Not checked` when its `Shown` line carries evidence or a `Not shown:` line names it, and `Should fix: no evidence for <promise>` when neither does.
- A promise the code keeps but `Shown` leaves out is at most `Nice to have`.
