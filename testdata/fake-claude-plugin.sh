#!/bin/sh
# Fake `claude plugin ...`: appends its argv as one line to $FAKE_PLUGIN_LOG.
# `plugin list --json` prints $FAKE_CLAUDE_LIST (a JSON array, default []),
# `plugin marketplace list --json` $FAKE_CLAUDE_MARKETPLACES (default []).
# With FAKE_CLAUDE_STATE (a dir) they print its plugins.json and
# marketplaces.json instead (default []), which `plugin marketplace add
# <path>` (the buildermesh marketplace reading <path>), `plugin marketplace
# remove` (it and its install gone) and `plugin install` (buildermesh@buildermesh
# at user scope, read from the marketplace's path) rewrite, as Claude Code does.
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
S=$FAKE_CLAUDE_STATE
show() {
  if [ -f "$S/$1" ]; then cat "$S/$1"; else echo '[]'; fi
}
if [ "$1" = plugin ] && [ "$2" = list ]; then
  if [ -n "$S" ]; then show plugins.json; else printf '%s\n' "${FAKE_CLAUDE_LIST:-[]}"; fi
fi
if [ "$1" = plugin ] && [ "$2" = marketplace ] && [ "$3" = list ]; then
  if [ -n "$S" ]; then show marketplaces.json; else printf '%s\n' "${FAKE_CLAUDE_MARKETPLACES:-[]}"; fi
fi
if [ -n "$S" ] && [ "$1" = plugin ]; then
  case "$2 $3" in
  "marketplace add")
    printf '%s\n' "$4" > "$S/path"
    printf '[{"name":"buildermesh","source":"directory","path":"%s"}]\n' "$4" > "$S/marketplaces.json" ;;
  "marketplace remove")
    rm -f "$S/path"
    echo '[]' > "$S/marketplaces.json"
    echo '[]' > "$S/plugins.json" ;;
  "install buildermesh@buildermesh")
    if [ ! -f "$S/path" ]; then echo "fake: no marketplace" >&2; exit 1; fi
    printf '[{"id":"buildermesh@buildermesh","scope":"user","enabled":true,"readFromFolder":"%s"}]\n' "$(cat "$S/path")" > "$S/plugins.json" ;;
  esac
fi
exit 0
