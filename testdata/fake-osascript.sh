#!/bin/sh
# Fake macOS osascript for the bmesh dialog: skips the -e script lines,
# takes the title and the body, appends "osascript <title>" to
# $FAKE_OSASCRIPT_LOG, writes the body to $FAKE_OSASCRIPT_BODY when set, and
# answers per $FAKE_OSASCRIPT_ANSWER: continue (default), cancel (exit 1,
# userCanceledErr), giveup (the dialog's timeout) or fail (no GUI).
while [ "$1" = -e ]; do shift 2; done
title=$1 body=$2
if [ -n "$FAKE_OSASCRIPT_LOG" ]; then echo "osascript $title" >> "$FAKE_OSASCRIPT_LOG"; fi
if [ -n "$FAKE_OSASCRIPT_BODY" ]; then printf '%s\n' "$body" > "$FAKE_OSASCRIPT_BODY"; fi
case ${FAKE_OSASCRIPT_ANSWER:-continue} in
  continue) echo "button returned:Continue to Keychain, gave up:false" ;;
  giveup) echo "button returned:, gave up:true" ;;
  cancel) echo "0:30: execution error: User canceled. (-128)" >&2; exit 1 ;;
  *) echo "0:30: execution error: No user interaction allowed. (-1713)" >&2; exit 1 ;;
esac
