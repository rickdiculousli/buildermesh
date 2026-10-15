#!/bin/sh
# Fake tmux: records each argv as one line in $FAKE_TMUX_LOG and exits 0;
# has-session exits 1 when FAKE_TMUX_NO_SESSION=1.
if [ -n "$FAKE_TMUX_LOG" ]; then echo "$*" >> "$FAKE_TMUX_LOG"; fi
if [ "$1" = has-session ] && [ "$FAKE_TMUX_NO_SESSION" = 1 ]; then exit 1; fi
exit 0
