#!/bin/sh
# Fake `claude plugin ...`: appends its argv as one line to $FAKE_PLUGIN_LOG.
# `plugin list --json` prints $FAKE_CLAUDE_LIST (a JSON array, default []).
# FAKE_PLUGIN_FAIL names a word (for example update) that, when it is the subcommand (second argument),
# (only checked as the second argument) makes the call print "fake failure" on stderr and exit with FAKE_PLUGIN_EXIT (default 0).
if [ -n "$FAKE_PLUGIN_LOG" ]; then printf '%s\n' "$*" >> "$FAKE_PLUGIN_LOG"; fi
if [ -n "$FAKE_PLUGIN_FAIL" ]; then
  for a in "$2"; do
    if [ "$a" = "$FAKE_PLUGIN_FAIL" ]; then
      echo "fake failure" >&2
      exit "${FAKE_PLUGIN_EXIT:-0}"
    fi
  done
fi
if [ "$1" = plugin ] && [ "$2" = list ]; then
  printf '%s\n' "${FAKE_CLAUDE_LIST:-[]}"
fi
exit 0
