#!/bin/sh
# seed.sh: run a throwaway bmesh serve (embedded NATS on a free loopback
# port, routines and ntfy off) with a seeded mesh, to look at the map. It
# prints the map URL and a CLI config for poking the mesh, and keeps serve
# running until Ctrl-C. State lives under ~/.cache/bmesh-scratch/seed and is
# wiped on each run. Arguments go to the seeder (scripts/seed), e.g. -leads 6.
set -eu

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
dir=${BMESH_SEED_DIR:-$HOME/.cache/bmesh-scratch/seed}
http=${BMESH_SEED_HTTP:-127.0.0.1:3775}

rm -rf "$dir/jetstream" "$dir/serve.log" "$dir/cli.toml" "$dir/app" "$dir/quota.json"
mkdir -p "$dir/app/docs/proposals" "$dir/app/.bmesh/routines"
cat >"$dir/app/docs/proposals/0042-map.md" <<'MD'
---
title: The map
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
git -C "$dir/app" init -q -b main
git -C "$dir/app" add -A
git -C "$dir/app" -c user.name=seed -c user.email=seed@localhost commit -q -m seed
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
BMESH_CONFIG="$dir/serve.toml" "$dir/bmesh" serve >"$dir/serve.log" 2>&1 &
pid=$!
trap 'kill "$pid" 2>/dev/null || true' EXIT
trap 'exit 130' INT TERM

# serve logs "bmesh serve: nats <url> (embedded), http <addr>, ..." once both listen.
url=
i=0
while [ -z "$url" ]; do
	kill -0 "$pid" 2>/dev/null || { cat "$dir/serve.log" >&2; echo "seed.sh: bmesh serve exited" >&2; exit 1; }
	i=$((i + 1))
	[ "$i" -le 30 ] || { echo "seed.sh: bmesh serve did not start; see $dir/serve.log" >&2; exit 1; }
	sleep 1
	url=$(sed -n 's/^bmesh serve: nats \([^ ]*\) (embedded).*/\1/p' "$dir/serve.log")
done

cat >"$dir/cli.toml" <<TOML
[mesh]
nats = "$url"
[host]
name = "seed"
TOML

(cd "$root" && go run ./scripts/seed -nats "$url" -map "http://$http/" -quota "$dir/quota.json" "$@")
printf 'cli: BMESH_CONFIG=%s %s --node app-0042 send app-0042.api hi\nserving until Ctrl-C (log: %s)\n' \
	"$dir/cli.toml" "$dir/bmesh" "$dir/serve.log"
wait "$pid"
