#!/bin/sh
# Fake macOS security: keeps generic-password items (service bmesh) as files
# $FAKE_SECURITY_DIR/<account> holding the password, and appends each argv
# as one line to $FAKE_SECURITY_LOG. Handles -i (one add-generic-password
# line on stdin, which is not logged), find-generic-password (-w prints the
# password; exit 44 when absent; exit 51 for -w when FAKE_SECURITY_DENY=1)
# and dump-keychain -a, whose entry 0 lists applications per
# $FAKE_SECURITY_ACL: empty (default), app (/usr/bin/security) or null.
if [ -n "$FAKE_SECURITY_LOG" ]; then echo "$*" >> "$FAKE_SECURITY_LOG"; fi
dir=${FAKE_SECURITY_DIR:?FAKE_SECURITY_DIR not set}
mkdir -p "$dir"

if [ "$1" = -i ]; then
  IFS= read -r line
  eval "set -- $line"
fi

cmd=$1; shift
svc= acct= label= pw= want_w=
while [ "$cmd" != dump-keychain ] && [ $# -gt 0 ]; do
  case $1 in
    -s) svc=$2; shift ;;
    -a) acct=$2; shift ;;
    -l) label=$2; shift ;;
    -T) shift ;;
    -U) ;;
    -w) if [ "$cmd" = add-generic-password ]; then pw=$2; shift; else want_w=1; fi ;;
  esac
  shift
done

case $cmd in
  add-generic-password)
    [ "$svc" = bmesh ] && [ -n "$acct" ] && [ -n "$pw" ] || { echo "bad add" >&2; exit 1; }
    printf '%s\n' "$pw" > "$dir/$acct"
    ;;
  find-generic-password)
    [ "$svc" = bmesh ] && [ -f "$dir/$acct" ] || {
      echo "security: SecKeychainSearchCopyNext: The specified item could not be found in the keychain." >&2
      exit 44
    }
    if [ -n "$want_w" ]; then
      if [ "$FAKE_SECURITY_DENY" = 1 ]; then
        echo "security: SecKeychainItemCopyContent: User interaction is not allowed." >&2
        exit 51
      fi
      cat "$dir/$acct"
    fi
    ;;
  dump-keychain)
    for f in "$dir"/*; do
      [ -f "$f" ] || continue
      a=$(basename "$f")
      cat <<END
keychain: "/Users/rick/Library/Keychains/login.keychain-db"
version: 512
class: "genp"
attributes:
    0x00000007 <blob>="bmesh $a key"
    0x00000008 <blob>=<NULL>
    "acct"<blob>="$a"
    "cdat"<timedate>=0x32303236313031363039303030305A00  "20261016090000Z\000"
    "svce"<blob>="bmesh"
access: 2 entries
    entry 0:
        authorizations (6): decrypt derive export_clear export_wrapped mac sign
        don't-require-password
        description: bmesh $a key
END
      case ${FAKE_SECURITY_ACL:-empty} in
        app)
          echo "        applications (1):"
          echo "            0: /usr/bin/security (OK)"
          echo "                requirement: identifier \"com.apple.security\" and anchor apple"
          ;;
        null) echo "        applications: <null>" ;;
        *) echo "        applications (0):" ;;
      esac
      cat <<END
    entry 1:
        authorizations (1): encrypt
        don't-require-password
        description: bmesh $a key
        applications: <null>
END
    done
    ;;
  *) echo "fake security: unknown command $cmd" >&2; exit 2 ;;
esac
exit 0
