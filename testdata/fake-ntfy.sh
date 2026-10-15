#!/bin/sh
# Fake ntfy command: appends $BMESH_TITLE, the stdin body and a "---" line
# to $FAKE_NTFY_LOG.
if [ -n "$FAKE_NTFY_LOG" ]; then
  { echo "$BMESH_TITLE"; cat; echo "---"; } >> "$FAKE_NTFY_LOG"
else
  cat > /dev/null
fi
exit 0
