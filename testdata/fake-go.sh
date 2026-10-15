#!/bin/sh
# Fake go for self-update tests: `build -o <out> <pkg>` appends
# "build <commit of the cwd>" to $FAKE_GO_LOG, sleeps $FAKE_GO_SLEEP seconds
# when set, then exits $FAKE_GO_EXIT when it is set and not 0 (printing
# "fake build failed"), else writes <out> as a script that prints
# "fake bmesh <commit>".
[ "$1" = build ] && [ "$2" = -o ] || { echo "fake go: want build -o <out> <pkg>, got $*" >&2; exit 2; }
out=$3
commit=$(git rev-parse HEAD)
[ -n "$FAKE_GO_LOG" ] && echo "build $commit" >> "$FAKE_GO_LOG"
[ -n "$FAKE_GO_SLEEP" ] && sleep "$FAKE_GO_SLEEP"
if [ -n "$FAKE_GO_EXIT" ] && [ "$FAKE_GO_EXIT" != 0 ]; then
  echo "fake build failed" >&2
  exit "$FAKE_GO_EXIT"
fi
printf '#!/bin/sh\necho fake bmesh %s\n' "$commit" > "$out"
