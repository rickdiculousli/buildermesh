#!/bin/sh
# Fake `agy plugin ...`: appends its argv as one line to $FAKE_PLUGIN_LOG.
# `plugin list --json` prints $FAKE_AGY_LIST (default []),
# `plugin install <dir>` succeeds unless FAKE_PLUGIN_FAIL=install.
if [ -n "$FAKE_PLUGIN_LOG" ]; then printf '%s\n' "$*" >> "$FAKE_PLUGIN_LOG"; fi
if [ -n "$FAKE_PLUGIN_FAIL" ]; then
  if [ "$2" = "$FAKE_PLUGIN_FAIL" ]; then
    echo "fake failure" >&2
    exit "${FAKE_PLUGIN_EXIT:-0}"
  fi
fi
if [ "$1" = plugin ] && [ "$2" = list ]; then
  list=$FAKE_AGY_LIST
  [ -n "$list" ] || list='[]'
  printf '%s\n' "$list"
elif [ "$1" = plugin ] && [ "$2" = install ]; then
  printf 'installed %s\n' "$3"
fi
exit 0
