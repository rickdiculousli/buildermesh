#!/bin/sh
# Fake `claude -p`: records argv in $FAKE_LOG and stdin in $FAKE_LOG.stdin,
# then prints a JSON result for the --session-id or --resume value.
# FAKE_RESULT is the result text (newlines and a literal \n decode as newlines),
# FAKE_CACHE the cache-read tokens, FAKE_IS_ERROR=1 sets is_error, FAKE_EXIT the exit code, FAKE_SLEEP a delay.
if [ -n "$FAKE_LOG" ]; then
  printf '%s\n' "$*" >> "$FAKE_LOG"
  cat >> "$FAKE_LOG.stdin"
else
  cat > /dev/null
fi
id=""
prev=""
for a in "$@"; do
  if [ "$prev" = "--session-id" ] || [ "$prev" = "--resume" ]; then id="$a"; fi
  prev="$a"
done
if [ -n "$FAKE_SLEEP" ]; then sleep "$FAKE_SLEEP"; fi
result=$(printf '%s' "$FAKE_RESULT" | sed 's/"/\\"/g' | awk 'NR > 1 { printf "\\n" } { printf "%s", $0 }')
iserr=false
if [ "$FAKE_IS_ERROR" = 1 ]; then iserr=true; fi
printf '%s\n' "{\"type\":\"result\",\"is_error\":$iserr,\"result\":\"$result\",\"session_id\":\"$id\",\"usage\":{\"input_tokens\":1000,\"cache_read_input_tokens\":${FAKE_CACHE:-0}}}"
exit "${FAKE_EXIT:-0}"
