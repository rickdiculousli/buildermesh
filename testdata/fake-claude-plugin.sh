#!/bin/sh
# Fake `claude plugin ...`: appends its argv as one line to $FAKE_PLUGIN_LOG.
# `plugin list --json` prints $FAKE_CLAUDE_LIST (a JSON array, default []).
# FAKE_PLUGIN_FAIL names a subcommand (the second argument, for example update) that prints
# "fake failure" on stderr and exits with FAKE_PLUGIN_EXIT.
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
