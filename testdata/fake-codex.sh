#!/bin/sh
# Fake `codex plugin ...`: appends its argv as one line to $FAKE_PLUGIN_LOG.
# `plugin list --json` prints $FAKE_CODEX_LIST (default {"installed":[]}),
# `plugin add <id> --json` prints {"version":"$FAKE_CODEX_ADD_VERSION"}.
# FAKE_PLUGIN_FAIL names a word (for example add) that, when it is the subcommand (second argument),
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
  printf '%s\n' "${FAKE_CODEX_LIST:-{\"installed\":[]\}}"
elif [ "$1" = plugin ] && [ "$2" = add ]; then
  printf '{"version":"%s"}\n' "$FAKE_CODEX_ADD_VERSION"
fi
exit 0
