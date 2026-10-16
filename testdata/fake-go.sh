#!/bin/sh
# Fake go for self-update tests: `build [-ldflags <flags>] -o <out> <pkg>`
# appends "build <commit of the cwd>" to $FAKE_GO_LOG, sleeps $FAKE_GO_SLEEP
# seconds when set, then exits $FAKE_GO_EXIT when it is set and not 0
# (printing "fake build failed"), else writes <out> as a script that prints
# "bmesh <rev8>" for `version` and "fake bmesh <commit>" otherwise. <rev8> is
# $FAKE_GO_REVISION when set, else the stampRevision in the flags (unknown
# without one).
[ "$1" = build ] || { echo "fake go: want build, got $*" >&2; exit 2; }
shift
flags=
if [ "$1" = -ldflags ]; then flags=$2; shift 2; fi
[ "$1" = -o ] || { echo "fake go: want -o <out> <pkg>, got $*" >&2; exit 2; }
out=$2
commit=$(git rev-parse HEAD)
[ -n "$FAKE_GO_LOG" ] && echo "build $commit" >> "$FAKE_GO_LOG"
[ -n "$FAKE_GO_SLEEP" ] && sleep "$FAKE_GO_SLEEP"
if [ -n "$FAKE_GO_EXIT" ] && [ "$FAKE_GO_EXIT" != 0 ]; then
  echo "fake build failed" >&2
  exit "$FAKE_GO_EXIT"
fi
rev=${FAKE_GO_REVISION:-$(printf '%s\n' "$flags" | sed -n 's/.*stampRevision=\([0-9a-f]*\).*/\1/p')}
rev=$(printf '%.8s' "${rev:-unknown}")
printf '#!/bin/sh\n[ "$1" = version ] && { echo "bmesh %s 2026-10-16T00:00:00Z"; exit 0; }\necho fake bmesh %s\n' "$rev" "$commit" > "$out"
