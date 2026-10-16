#!/bin/sh
# Fake go for self-update tests: `build [-ldflags <flags>] -o <out> <pkg>`
# appends "build <commit of the cwd>" to $FAKE_GO_LOG, sleeps $FAKE_GO_SLEEP
# seconds when set, then exits $FAKE_GO_EXIT when it is set and not 0
# (printing "fake build failed"), else writes <out> as a script that prints
# "bmesh <rev8>" for `version` ("bmesh <release> (<rev8> ...)" when the flags
# stamp a stampRelease) and "fake bmesh <commit>" otherwise. <rev8> is
# $FAKE_GO_REVISION when set, else the stampRevision in the flags (unknown
# without one).
# `install [-ldflags <flags>] <module>/cmd/bmesh@<commit>` appends "install
# <module>@<commit>" to $FAKE_GO_LOG and, with the same $FAKE_GO_EXIT, writes
# $GOBIN/bmesh printing "bmesh <rev8>" for `version` and "fake bmesh
# <commit>" otherwise, <rev8> being $FAKE_GO_REVISION, else the stamp, else
# the commit as a pseudo-version names it (unknown with $FAKE_GO_TAGGED set:
# go resolved the commit to a tag, which names none).
if [ "$1" = install ]; then
  shift
  flags=
  if [ "$1" = -ldflags ]; then flags=$2; shift 2; fi
  pkg=${1%@*}
  commit=${1##*@}
  [ -n "$FAKE_GO_LOG" ] && echo "install ${pkg%/cmd/bmesh}@$commit" >> "$FAKE_GO_LOG"
  if [ -n "$FAKE_GO_EXIT" ] && [ "$FAKE_GO_EXIT" != 0 ]; then
    echo "fake install failed" >&2
    exit "$FAKE_GO_EXIT"
  fi
  [ -n "$GOBIN" ] || { echo "fake go: no GOBIN" >&2; exit 2; }
  stamped=$(printf '%s\n' "$flags" | sed -n 's/.*stampRevision=\([0-9a-f]*\).*/\1/p')
  named=$commit
  [ -n "$FAKE_GO_TAGGED" ] && named=unknown
  rev=$(printf '%.8s' "${FAKE_GO_REVISION:-${stamped:-$named}}")
  printf '#!/bin/sh\n[ "$1" = version ] && { echo "bmesh %s 2026-10-16T00:00:00Z"; exit 0; }\necho fake bmesh %s\n' "$rev" "$commit" > "$GOBIN/bmesh"
  chmod +x "$GOBIN/bmesh"
  exit 0
fi
[ "$1" = build ] || { echo "fake go: want build or install, got $*" >&2; exit 2; }
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
release=$(printf '%s\n' "$flags" | sed -n 's/.*stampRelease=\([^ ]*\).*/\1/p')
said="$rev 2026-10-16T00:00:00Z"
[ -n "$release" ] && said="$release ($said)"
printf '#!/bin/sh\n[ "$1" = version ] && { echo "bmesh %s"; exit 0; }\necho fake bmesh %s\n' "$said" "$commit" > "$out"
