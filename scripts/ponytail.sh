#!/usr/bin/env bash
# ponytail.sh [check | level [TAG]]: keep bmesh's copy of ponytail
# (https://github.com/DietrichGebert/ponytail, MIT) level with its releases.
#
#   scripts/ponytail.sh check    # exit 0 when the copy is at the latest release, 1 when a newer one is out
#   scripts/ponytail.sh level    # copy the latest release (or TAG) in; read the diff, verify, commit
#
# The copies are ponytail's own text, never edited by hand: a change to them
# belongs upstream. third_party/ponytail/PIN names the release and every copy.
# Read every diff a level makes before committing it: the copies become the
# instructions of every agent that loads them. The `ponytail` rule set is
# upstream's AGENTS.md without its title and its closing line, its section
# headings as bold lines, and a credit line; the manual's Rule sets section
# holds the same text (go test ./internal/scaffold fails until it does).
set -euo pipefail

url=https://github.com/DietrichGebert/ponytail
root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
pin=$root/third_party/ponytail/PIN

# The whole list, then its first line: a pipe closed early would fail under pipefail.
tags=$(git ls-remote --tags --refs --sort=-version:refname "$url" 'v*')
latest=${tags%%$'\n'*}
latest=${latest##*refs/tags/}
if [ -z "$latest" ]; then
	echo "ponytail.sh: no release tag at $url" >&2
	exit 2
fi
pinned=$(awk '$1 == "release" { print $2 }' "$pin" 2>/dev/null || true)

case "${1:-check}" in
check)
	if [ "$pinned" = "$latest" ]; then
		echo "ponytail $pinned: level with the latest release"
		exit 0
	fi
	echo "ponytail ${pinned:-not copied}: $latest is out; run scripts/ponytail.sh level"
	exit 1
	;;
level)
	tag=${2:-$latest}
	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT
	git -c advice.detachedHead=false clone -q --depth 1 --branch "$tag" "$url" "$tmp/src"
	src=$tmp/src
	mkdir -p "$root/third_party/ponytail" "$root/skills/ponytail-review"
	cp "$src/LICENSE" "$root/third_party/ponytail/LICENSE"
	cp "$src/skills/ponytail-review/SKILL.md" "$root/skills/ponytail-review/SKILL.md"
	{
		awk 'NR == 1 && /^# / { next }
			/^\(Yes, this file also applies/ { exit }
			/^## / { sub(/^## /, ""); $0 = "**" $0 "**" }
			/^[ \t]*$/ { blank = 1; next }
			{ if (started && blank) print ""; blank = 0; started = 1; print }' "$src/AGENTS.md"
		printf '\nFrom [ponytail](%s) %s by DietrichGebert (MIT).\n' "$url" "$tag"
	} >"$root/internal/scaffold/rules/ponytail.md"
	cat >"$pin" <<EOF
# bmesh's copy of ponytail, $url (MIT, LICENSE beside this file).
# scripts/ponytail.sh level writes this file and every file it names; edit none by hand.
release $tag
commit $(git -C "$src" rev-parse HEAD)
copy LICENSE third_party/ponytail/LICENSE
copy skills/ponytail-review/SKILL.md skills/ponytail-review/SKILL.md
rules AGENTS.md internal/scaffold/rules/ponytail.md
EOF
	echo "ponytail $tag copied; read the diff before committing it:"
	git -C "$root" status --short -- third_party/ponytail skills/ponytail-review internal/scaffold/rules/ponytail.md
	;;
*)
	echo "usage: scripts/ponytail.sh [check | level [TAG]]" >&2
	exit 2
	;;
esac
