#!/bin/sh
# Fake tmux: records each argv as one line in $FAKE_TMUX_LOG and exits 0;
# has-session exits 1 when FAKE_TMUX_NO_SESSION=1. capture-pane prints a
# prompt line, or, when $FAKE_TMUX_PANES names a directory, its files 1, 2,
# 3... one per call (the last one again once they run out), so a test can
# script what claude shows over time.
if [ -n "$FAKE_TMUX_LOG" ]; then echo "$*" >> "$FAKE_TMUX_LOG"; fi
if [ "$1" = has-session ] && [ "$FAKE_TMUX_NO_SESSION" = 1 ]; then exit 1; fi
if [ "$1" = capture-pane ]; then
  if [ -n "$FAKE_TMUX_PANES" ]; then
    n=$(cat "$FAKE_TMUX_PANES/.n" 2>/dev/null || echo 0); n=$((n + 1))
    [ -f "$FAKE_TMUX_PANES/$n" ] || n=$((n - 1))
    echo "$n" > "$FAKE_TMUX_PANES/.n"
    cat "$FAKE_TMUX_PANES/$n"
  else
    echo "auto mode on (shift+tab to cycle)"
  fi
fi
exit 0
