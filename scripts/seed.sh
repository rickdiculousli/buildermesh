#!/bin/sh
# seed.sh: start a throwaway bmesh serve (embedded NATS and HTTP on free
# loopback ports, routines and ntfy off) in the background with a seeded mesh
# holding every state the map draws, print the map's URL and a CLI config for
# poking the mesh, and return; serve keeps running until `seed.sh stop`. The
# page (index.html, map.js, map.css) is read from this checkout's
# internal/web on every request (BMESH_WEB_DIR), so an edit shows on reload
# without a restart; Go changes need a re-run. State lives under
# ~/.cache/bmesh-scratch/seed/<checkout name> and is wiped on each run (serve's
# state dir too, so it never writes the box's serve record or plugin);
# seed.pid there holds serve's pid. The seeded mesh also holds token use (the
# bm-usage stream: leads, an executor, a routine, two coordinator sessions and
# a day of quota readings) and the app repo's specs carry a size, over a
# month of back-dated commits for the burndowns (specs proposed and landed
# with Spec: trailers on several days).
# A run stops the previous one of the same checkout first. Other arguments go
# to the seeder (scripts/seed), e.g. -leads 6. BMESH_SEED_HTTP sets serve's listen address. Serve reads no
# transcripts: its Claude Code config dir is an empty one.
set -eu

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
dir=${BMESH_SEED_DIR:-$HOME/.cache/bmesh-scratch/seed/$(basename -- "$root")}
http=${BMESH_SEED_HTTP:-127.0.0.1:0}
pidfile="$dir/seed.pid"

# stop kills the pid in the pidfile when it still runs this seed's serve,
# and waits for it to go; never pkill -f, which matches the caller's own
# shell.
stop() {
	[ -f "$pidfile" ] || return 0
	pids=
	for p in $(cat "$pidfile"); do
		case $(ps -o args= -p "$p" 2>/dev/null) in
		*"$dir/bmesh serve"*) pids="$pids $p" ;;
		esac
	done
	for p in $pids; do kill "$p" 2>/dev/null || true; done
	i=0
	for p in $pids; do
		while kill -0 "$p" 2>/dev/null && [ "$i" -lt 50 ]; do
			i=$((i + 1))
			sleep 0.1
		done
	done
	rm -f "$pidfile"
}

if [ "${1:-}" = stop ]; then
	stop
	echo "seed.sh: stopped"
	exit 0
fi
stop

rm -rf "$dir/jetstream" "$dir/state" "$dir/serve.log" "$dir/cli.toml" "$dir/app" "$dir/quota.json"
mkdir -p "$dir/app/docs/proposals" "$dir/app/.bmesh/routines"
# spec ID SIZE STATUS TITLE writes a short spec: the other leads' specs,
# for the sizes the usage table and the flags' size rule read.
spec() {
	printf -- '---\nid: %s\ntitle: %s\nsize: %s\nstatus: %s\n---\n# %s\n' "$1" "$4" "$2" "$3" "$4" >"$dir/app/docs/proposals/00$1-$4.md"
}
# commit DAYS MSG commits everything as of DAYS days ago: the history the
# burndowns read (a spec's proposed day is its file's first commit, its
# landed day the commit whose Spec: trailer names it).
now=$(date +%s)
commit() {
	t="@$((now - $1 * 86400)) +0000"
	git -C "$dir/app" add -A
	GIT_AUTHOR_DATE=$t GIT_COMMITTER_DATE=$t git -C "$dir/app" -c user.name=seed -c user.email=seed@localhost commit -q -m "$2"
}
git -C "$dir/app" init -q -b main
# A month of specs: app-batch-1's four (30 to 33) and four more proposed
# 26 days ago, landed over the month; 39 to 42 proposed 12 days ago, 45 to
# 47 five days ago.
for id in 30 31 32 33 34 35 36 37; do spec "$id" S accepted "spec-$id"; done
commit 26 "Propose 30 to 37"
land() { # land DAYS ID...: each ID done in its own commit with its trailer
	d=$1
	shift
	for id; do
		spec "$id" S done "spec-$id"
		commit "$d" "$(printf 'Spec %s\n\nSpec: %s' "$id" "$id")"
	done
}
land 20 34
land 16 35 36
spec 39 S accepted legend
spec 40 L accepted hub-move
spec 41 S accepted parser
spec 42 M accepted map
commit 12 "Propose 39 to 42"
land 9 37
spec 45 M accepted fan-out
spec 46 M proposed cache
spec 47 S proposed retry
commit 5 "Propose 45 to 47"
land 3 30 31 32 33
spec 40 L building hub-move
spec 41 S building parser
spec 45 M building fan-out
spec 39 S done legend
commit 0 "$(printf 'Spec 39\n\nSpec: 39')"
cat >"$dir/app/docs/proposals/0042-map.md" <<'MD'
---
id: 42
title: The map
size: M
status: building
merged: 0123abcd4567ef89
verified: review ok, verify ok at 0123abcd
---
# The map

A page on the daemon that draws the mesh as a tree.

## Endpoints

- /api/tree, /api/lead/{path}, /api/routines, /api/strip
- /events replays the last day

```
curl -s http://127.0.0.1:3775/api/tree | jq .
```

## Done when

The map works at 390x844 and 1280x800.
MD
# A spec app-round-1's engineer landed (the seed's thread-round-1-eng).
cat >"$dir/app/docs/proposals/043-faster-tests.md" <<'MD'
---
id: 043
title: Tests share one hub
kind: test-speed
size: S
judge: sonnet
status: proposed
lens: eng
proposed_by: thread-round-1-eng
proposed_on: 2026-10-15
---

## Opportunity
Each test starts its own hub.

## Proposal
One hub per package.
MD
# A landed spec with steps for the operator still open: a band row.
cat >"$dir/app/docs/proposals/044-retire-old-hub.md" <<'MD'
---
id: 044
title: Retire the old hub
kind: maintenance
size: M
judge: opus
status: done
lens: eng
operator_steps: [{"n":1,"box":"laptop","text":"unload the old hub's service","check":"test ! -e ~/old-hub.plist","done":"2026-10-15T09:00:00Z laptop"},{"n":2,"box":"laptop","text":"move the token to the new config dir","done":""},{"n":3,"box":"","text":"archive the old repo","done":""}]
---

## Needs from the operator
- [laptop] unload the old hub's service check: `test ! -e ~/old-hub.plist`
- [laptop] move the token to the new config dir
- archive the old repo
MD
cat >"$dir/app/.bmesh/routines/review.toml" <<'TOML'
mode = "pooled"
model = "claude-opus-5-5"
prompt = "Review the change."
TOML
cat >"$dir/app/.bmesh/routines/verify.toml" <<'TOML'
mode = "inline"
deterministic = true
command = "go test ./..."
TOML
cat >"$dir/app/.bmesh/routines/surface.toml" <<'TOML'
mode = "fresh"
model = "claude-opus-5-5"
prompt = "Screenshot the surface."
TOML
commit 0 seed
cat >"$dir/serve.toml" <<TOML
[mesh]
nats = ""
store = "$dir/jetstream"
listen = "127.0.0.1:0"
[host]
name = "seed"
[serve]
listen = "$http"
[runner]
checkouts = ""
[notify]
ntfy = ""
[quota]
command = "cat '$dir/quota.json'"
[console]
thread_url = "http://127.0.0.1:3773/env/{thread}"
TOML
cat > "$dir/repos.toml" <<TOML
[repos.app]
path = "$dir/app"
TOML

(cd "$root" && go build -o "$dir/bmesh" ./cmd/bmesh)
# Detached (its own session where setsid exists, stdin closed, output to the
# log) so it outlives this script and the shell that ran it.
detach=
command -v setsid >/dev/null 2>&1 && detach=setsid
(
	XDG_STATE_HOME="$dir/state" BMESH_CONFIG="$dir/serve.toml" CLAUDE_CONFIG_DIR="$dir/claude" BMESH_WEB_DIR=${BMESH_WEB_DIR:-$root/internal/web} \
		nohup $detach "$dir/bmesh" serve </dev/null >"$dir/serve.log" 2>&1 &
	echo $! >"$pidfile"
)
pid=$(cat "$pidfile")
fail() {
	echo "seed.sh: $1; see $dir/serve.log" >&2
	stop
	exit 1
}

# serve logs "bmesh serve: nats <url> (embedded), http <addr>, ..." once both listen.
url=
i=0
while [ -z "$url" ]; do
	kill -0 "$pid" 2>/dev/null || { cat "$dir/serve.log" >&2; fail "bmesh serve exited"; }
	i=$((i + 1))
	[ "$i" -le 30 ] || fail "bmesh serve did not start"
	sleep 1
	url=$(sed -n 's/^bmesh serve: nats \([^ ]*\) (embedded), http \([^,]*\),.*/\1/p' "$dir/serve.log")
done
addr=$(sed -n 's/^bmesh serve: nats [^ ]* (embedded), http \([^,]*\),.*/\1/p' "$dir/serve.log")

cat >"$dir/cli.toml" <<TOML
[mesh]
nats = "$url"
[host]
name = "seed"
TOML

(cd "$root" && go run ./scripts/seed -nats "$url" -map "http://$addr/" -quota "$dir/quota.json" "$@") || fail "seeding failed"
printf 'check: node %s/scripts/check.mjs "http://%s/?check=1" 390 844 shot.png\n' "$root" "$addr"
printf 'cli: BMESH_CONFIG=%s %s --node app-0042 send app-0042.api hi\nserving until %s stop (pid %s, log: %s)\n' \
	"$dir/cli.toml" "$dir/bmesh" "$0" "$pid" "$dir/serve.log"
