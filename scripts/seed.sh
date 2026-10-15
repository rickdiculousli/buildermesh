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

rm -rf "$dir/jetstream" "$dir/serve.log" "$dir/cli.toml"
mkdir -p "$dir"
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

(cd "$root" && go run ./scripts/seed -nats "$url" -map "http://$http/" "$@")
printf 'cli: BMESH_CONFIG=%s %s --node app-0042 send app-0042.api hi\nserving until Ctrl-C (log: %s)\n' \
	"$dir/cli.toml" "$dir/bmesh" "$dir/serve.log"
wait "$pid"
