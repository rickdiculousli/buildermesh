#!/bin/sh
# Fake t3_dispatch.py: records argv, and prompt files for send/start, in $FAKE_T3_LOG.
cmd="$1"; id="$2"
if [ -n "$FAKE_T3_LOG" ]; then echo "argv $*" >> "$FAKE_T3_LOG"; fi
case "$cmd" in
status)
  if [ "$FAKE_T3_MISSING" = 1 ]; then echo "no such thread" >&2; exit 3; fi
  echo "{\"id\":\"$id\",\"session\":{\"status\":\"${FAKE_T3_STATUS:-idle}\"},\"latestTurn\":{\"state\":\"${FAKE_T3_TURN:-completed}\"},\"lastReply\":\"${FAKE_T3_REPLY:-}\"}"
  ;;
send)
  file=""
  prev=""
  for a in "$@"; do
    if [ "$prev" = "--prompt-file" ]; then file="$a"; fi
    prev="$a"
  done
  if [ -n "$FAKE_T3_LOG" ]; then echo "send $id $(cat "$file" 2>/dev/null)" >> "$FAKE_T3_LOG"; fi
  exit "${FAKE_T3_SEND_EXIT:-0}"
  ;;
start)
  file=""
  tid="${FAKE_T3_THREAD:-t-fake}"
  prev=""
  for a in "$@"; do
    if [ "$prev" = "--prompt-file" ]; then file="$a"; fi
    if [ "$prev" = "--thread-id" ]; then tid="$a"; fi
    prev="$a"
  done
  if [ -n "$FAKE_T3_LOG" ]; then echo "start $(cat "$file" 2>/dev/null | head -c 2000)" >> "$FAKE_T3_LOG"; fi
  if [ "${FAKE_T3_START_EXIT:-0}" != 0 ]; then echo "start failed" >&2; exit "$FAKE_T3_START_EXIT"; fi
  echo "{\"threadId\":\"$tid\",\"title\":\"x\",\"branch\":\"t3code/$tid\",\"worktreePath\":\"${FAKE_T3_WORKTREE:-/wt}/$tid\"}"
  ;;
stop)
  if [ -n "$FAKE_T3_LOG" ]; then echo "stop $id" >> "$FAKE_T3_LOG"; fi
  echo "{\"threadId\":\"$id\",\"stopped\":true}"
  ;;
*) echo "unknown $cmd" >&2; exit 1 ;;
esac
