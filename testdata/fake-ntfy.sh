#!/bin/sh
# Fake ntfy command: appends $BMESH_TITLE, the stdin body, then (when
# $NTFY_TITLE is set) a newline and the NTFY_* headers one per line, and a
# "---" line to $FAKE_NTFY_LOG.
if [ -n "$FAKE_NTFY_LOG" ]; then
  {
    echo "$BMESH_TITLE"
    cat
    if [ -n "$NTFY_TITLE" ]; then
      echo
      echo "NTFY_TITLE=$NTFY_TITLE"
      echo "NTFY_PRIORITY=$NTFY_PRIORITY"
      echo "NTFY_TAGS=$NTFY_TAGS"
      echo "NTFY_CLICK=$NTFY_CLICK"
      echo "NTFY_ACTIONS=$NTFY_ACTIONS"
    fi
    echo "---"
  } >> "$FAKE_NTFY_LOG"
else
  cat > /dev/null
fi
exit 0
