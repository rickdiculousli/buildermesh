#!/bin/sh
# Fake ssh: records the host and command line in $FAKE_SSH_LOG, exits
# $FAKE_SSH_EXIT (default 0) and prints $FAKE_SSH_OUTPUT.
while [ $# -gt 0 ]; do
  case "$1" in
    -o) shift 2 ;;
    *) break ;;
  esac
done
host="$1"; shift
if [ -n "$FAKE_SSH_LOG" ]; then echo "ssh $host $*" >> "$FAKE_SSH_LOG"; fi
if [ "${FAKE_SSH_EXIT:-0}" != 0 ]; then echo "ssh: connect to host $host: no route" >&2; exit "$FAKE_SSH_EXIT"; fi
if [ -n "$FAKE_SSH_OUTPUT" ]; then printf '%s\n' "$FAKE_SSH_OUTPUT"; fi
