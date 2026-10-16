#!/bin/sh
# Fake bmesh serve for the supervisor's tests: appends one line, its
# arguments and BMESH_SUPERVISED ("run <args> supervised=<v>"), to
# $FAKE_SERVE_LOG and prints "fake serve output"; with $FAKE_SERVE_HOLD set
# it runs until SIGTERM, appending "term", else it exits $FAKE_SERVE_EXIT
# (0 when unset).
echo "run $* supervised=$BMESH_SUPERVISED" >> "$FAKE_SERVE_LOG"
echo "fake serve output"
if [ -n "$FAKE_SERVE_HOLD" ]; then
	trap 'echo term >> "$FAKE_SERVE_LOG"; exit 0' TERM
	while :; do sleep 0.05; done
fi
exit "${FAKE_SERVE_EXIT:-0}"
