#!/usr/bin/env bash
# memcap.sh N: measure the memory N leads take on this box, to set [runner] max_leads.
#
# It starts a bmesh daemon of its own (embedded NATS on a loopback port, no
# ntfy, nothing shared with the real mesh), a throwaway git repo, and N leads
# on the tmux runner, each briefed to spawn three executors that append a
# line to a file every 30 s for 5 minutes and report. Every 10 s it samples
# the RSS of every claude process (and of its process tree: MCP servers and
# the like), then prints the peak total, the per-process median at the peak
# and the peak tree total, checks that every lead and executor reached the
# mesh, and tears everything down. Linux only (procps ps).
#
#   scripts/memcap.sh 3            # one run; the summary line is the last line
#   MEMCAP_SECONDS=300 scripts/memcap.sh 4
#
# Needs bmesh, claude and tmux on PATH. T3 should be idle so the numbers are
# the leads' alone. Output and samples go under ~/.cache/bmesh-memcap/.
set -euo pipefail

N=${1:?usage: memcap.sh N}
SECONDS_TOTAL=${MEMCAP_SECONDS:-420}
ROOT=${MEMCAP_ROOT:-$HOME/.cache/bmesh-memcap}
STAMP=$(date +%Y%m%d-%H%M%S)
RUN=$ROOT/run-$N-$STAMP
SAMPLES=$ROOT/memcap-$N-$STAMP.csv
PORT=${MEMCAP_NATS_PORT:-4322}
HTTP=${MEMCAP_HTTP_PORT:-3775}
SESSION=memcap
mkdir -p "$RUN"

cleanup() {
  tmux kill-session -t "$SESSION" 2>/dev/null || true
  if [ -n "${SERVE_PID:-}" ]; then kill "$SERVE_PID" 2>/dev/null || true; wait "$SERVE_PID" 2>/dev/null || true; fi
  rm -rf "$RUN"
}
trap cleanup EXIT

if tmux has-session -t "$SESSION" 2>/dev/null; then
  echo "memcap: tmux session $SESSION exists; a run is in progress" >&2
  exit 1
fi

# The throwaway repo the leads work in.
REPO=$RUN/scratch
git init -q -b main "$REPO"
echo "memcap scratch" > "$REPO/README.md"
git -C "$REPO" -c user.name=memcap -c user.email=memcap@example add -A
git -C "$REPO" -c user.name=memcap -c user.email=memcap@example commit -qm init

# Two configs: the daemon embeds NATS, the CLI and the leads dial it.
cat > "$RUN/serve.toml" <<TOML
[mesh]
nats = ""
store = "$RUN/jetstream"
listen = "127.0.0.1:$PORT"
[host]
name = "memcap"
[serve]
listen = "127.0.0.1:$HTTP"
[runner]
kind = "tmux"
tmux_session = "$SESSION"
worktrees = "$RUN/worktrees"
checkouts = ""
[notify]
ntfy = ""
[repos]
scratch = "$REPO"
TOML
sed -e "s|^nats = \"\"|nats = \"nats://127.0.0.1:$PORT\"|" "$RUN/serve.toml" > "$RUN/cli.toml"

BMESH_CONFIG=$RUN/serve.toml bmesh serve > "$RUN/serve.log" 2>&1 &
SERVE_PID=$!
for _ in $(seq 1 50); do
  if BMESH_CONFIG=$RUN/cli.toml bmesh status >/dev/null 2>&1; then break; fi
  sleep 0.2
done
BMESH_CONFIG=$RUN/cli.toml bmesh status >/dev/null

# One lead per N. The brief is one line: the tmux runner types it in, and a
# newline would submit it early. 10 x 30 s keeps each executor busy 5 min.
for i in $(seq 1 "$N"); do
  node=memcap-$i
  brief=$RUN/brief-$i.md
  printf '%s' "You are lead $node in a buildermesh memory measurement; everything here is throwaway and nobody will answer questions, so never ask one. Your worktree is this directory and bmesh resolves your node from it. Do exactly this, nothing more: run \`bmesh started --text memcap\`; then for each NAME in a, b, c run \`bmesh executor new --name NAME --json\` and read its worktree and branch from the JSON, then spawn an Agent subagent (run_in_background true, subagent_type general-purpose) whose whole prompt is: 'You are executor $node.NAME in a throwaway memory measurement. Every shell command you run starts with cd WORKTREE && (fill in the worktree; the working directory does not persist). Run \`cd WORKTREE && bmesh --node $node.NAME started --text memcap\`. Then run exactly this command once, with the Bash tool timeout set to 400000: \`cd WORKTREE && for i in \$(seq 1 10); do date >> notes.txt; sleep 30; done\`. Then run \`cd WORKTREE && git add -A && git -c user.name=memcap -c user.email=memcap@example commit -qm notes\`, then \`cd WORKTREE && bmesh --node $node.NAME send $node \"branch BRANCH done\"\` and \`cd WORKTREE && bmesh --node $node.NAME done --commit \$(git rev-parse HEAD)\`. Reply with one word.' Spawn all three before waiting on any. When all three have finished, run \`bmesh inbox\`, then \`bmesh progress --text finished\`, and reply with one line." > "$brief"
  BMESH_CONFIG=$RUN/cli.toml bmesh lead start --repo scratch --spec "00$i" --brief "$brief" --path "$node" --title "memcap $i" \
    >> "$RUN/leads.log" 2>&1 || { echo "memcap: lead $i failed to start:" >&2; cat "$RUN/leads.log" >&2; exit 1; }
done
echo "memcap: $N leads started; sampling for ${SECONDS_TOTAL}s (samples in $SAMPLES)"

# Sample. Each line: elapsed seconds, claude process count, claude RSS total
# (KB), per-process RSS list, tree RSS total (KB). Only processes under the
# memcap session's panes count, so other claude sessions on the box don't.
echo "elapsed,claude_procs,claude_rss_kb,per_process_kb,tree_rss_kb" > "$SAMPLES"
start=$(date +%s)
peak_total=0; peak_procs=0; peak_list=""; peak_tree=0
while :; do
  now=$(date +%s); elapsed=$((now - start))
  if [ "$elapsed" -ge "$SECONDS_TOTAL" ]; then break; fi
  snapshot=$(ps -eo pid=,ppid=,rss=,comm=)
  pane_pids=$(tmux list-panes -s -t "$SESSION" -F '#{pane_pid}' 2>/dev/null | tr '\n' ' ')
  claude_pids=$(echo "$snapshot" | awk -v roots="$pane_pids" '
    BEGIN { n = split(roots, r, " "); for (i = 1; i <= n; i++) root[r[i]] = 1 }
    { ppid[$1] = $2; comm[$1] = $4 }
    END {
      for (p in ppid) { if (comm[p] != "claude") continue; q = p
        while (q in ppid) { if (q in root) { print p; break }; q = ppid[q] } }
    }' | tr '\n' ' ')
  total=0; procs=0; list=""
  for pid in $claude_pids; do
    rss=$(echo "$snapshot" | awk -v p="$pid" '$1==p{print $3}')
    total=$((total + rss)); procs=$((procs + 1)); list="$list${list:+ }$rss"
  done
  # Tree: every process whose ancestor chain reaches a claude process.
  tree=$(echo "$snapshot" | awk -v roots="$claude_pids" '
    BEGIN { n = split(roots, r, " "); for (i = 1; i <= n; i++) root[r[i]] = 1 }
    { ppid[$1] = $2; rss[$1] = $3 }
    END {
      for (p in ppid) { q = p; while (q in ppid) { if (q in root) { t += rss[p]; break }; q = ppid[q] } }
      print t + 0
    }')
  echo "$elapsed,$procs,$total,\"$list\",$tree" >> "$SAMPLES"
  if [ "$total" -gt "$peak_total" ]; then peak_total=$total; peak_procs=$procs; peak_list=$list; fi
  if [ "$tree" -gt "$peak_tree" ]; then peak_tree=$tree; fi
  sleep 10
done

# The numbers mean something only if every node did its work.
status=$ROOT/memcap-$N-$STAMP-status.txt
BMESH_CONFIG=$RUN/cli.toml bmesh status --json > "$status" 2>&1 || true
reached=$(python3 - "$status" <<'PY'
import json, sys
rows = json.load(open(sys.argv[1]))
leads = sum(1 for r in rows if r["node"]["role"] == "lead" and r["state"]["state"] in ("working", "done"))
execs = sum(1 for r in rows if r["node"]["role"] == "executor" and r["state"]["state"] in ("working", "done"))
print(f"{leads} {execs}")
PY
) || reached="? ?"
set -- $reached
if [ "$1" != "$N" ] || [ "$2" != "$((N * 3))" ]; then
  echo "memcap N=$N: INVALID: $1 of $N leads and $2 of $((N * 3)) executors reached working/done (see $status)"
  exit 1
fi
median=$(echo "$peak_list" | tr ' ' '\n' | sort -n | awk '{a[NR]=$1} END { if (NR == 0) print 0; else if (NR % 2) print a[(NR+1)/2]; else print int((a[NR/2] + a[NR/2+1]) / 2) }')
echo "memcap N=$N: peak claude total $((peak_total / 1024)) MB over $peak_procs processes, per-process median $((median / 1024)) MB at the peak, peak tree total $((peak_tree / 1024)) MB, ${SECONDS_TOTAL}s"
