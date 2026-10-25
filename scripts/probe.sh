#!/usr/bin/env bash
# Manual Obsidian probes (docs/probes/CHECKLIST.md).
#
# Copies the made-up vault in testdata/probes/vault into a scratch folder,
# snapshots it after each manual step, and diffs consecutive snapshots.
# Refuses to touch anything outside $TMPDIR, so it can never reach a real vault.
#
#   scripts/probe.sh setup          create the scratch vault, print its path
#   scripts/probe.sh path           print the scratch vault path
#   scripts/probe.sh snap <label>   snapshot and diff against the previous one
#   scripts/probe.sh ext <atomic|inplace>
#                                   append a line to pr08/Watch.md from outside Obsidian
#   scripts/probe.sh clean          delete the scratch folder
set -euo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(cd "${TMPDIR:?TMPDIR is not set}" && pwd -P)
base="$tmp/obs-probe"
vault="$base/ObsProbe"
snaps="$base/snaps"

guard() {
	local real
	case "$tmp" in
	/private/var/folders/* | /var/folders/* | /private/tmp | /private/tmp/* | /tmp | /tmp/*) ;;
	*) echo "refusing: TMPDIR ($tmp) is not a system temp directory" >&2; exit 1 ;;
	esac
	real=$(cd "$base" 2>/dev/null && pwd -P || echo "$base")
	case "$real" in
	"$tmp"/*) ;;
	*) echo "refusing: $real is not under $tmp" >&2; exit 1 ;;
	esac
}

latest() { ls -1 "$snaps" 2>/dev/null | sort | tail -n 1; }

snapshot() {
	local n label="$1"
	n=$(ls -1 "$snaps" 2>/dev/null | wc -l | tr -d ' ')
	local dir
	dir=$(printf '%s/%02d-%s' "$snaps" "$n" "$label")
	rsync -a "$vault/" "$dir/"
	echo "$dir"
}

guard
cmd=${1:-}
case "$cmd" in
setup)
	if [ -e "$vault" ]; then echo "already exists: $vault (run clean first)" >&2; exit 1; fi
	mkdir -p "$snaps"
	rsync -a "$repo/testdata/probes/vault/" "$vault/"
	snapshot baseline >/dev/null
	echo "$vault"
	;;
path)
	echo "$vault"
	;;
snap)
	label=${2:?usage: snap <label>}
	[ -d "$vault" ] || { echo "no vault; run setup" >&2; exit 1; }
	prev="$snaps/$(latest)"
	cur=$(snapshot "$label")
	echo "== $(basename "$cur") vs $(basename "$prev")"
	diff -ruN -x workspace.json -x workspace-mobile.json -x '.DS_Store' -x 'probe-dump-*.json' \
		"$prev" "$cur" | sed "s#$snaps/##g" || true
	for d in "$cur"/probe-dump-*.json; do
		[ -e "$d" ] && echo "dump: $d ($(wc -c <"$d" | tr -d ' ') bytes)"
	done
	;;
ext)
	f="$vault/pr08/Watch.md"
	line="EXT ${2:?usage: ext <atomic|inplace>} $(date +%H:%M:%S)"
	case "$2" in
	atomic)
		t="$vault/pr08/.Watch.md.tmp"
		{ cat "$f"; echo "$line"; } >"$t"
		mv -f "$t" "$f"
		;;
	inplace)
		c=$(cat "$f")
		printf '%s\n%s\n' "$c" "$line" >"$f"
		;;
	*) echo "unknown mode $2" >&2; exit 1 ;;
	esac
	echo "wrote: $line"
	;;
clean)
	rm -rf "$base"
	echo "removed $base"
	;;
*)
	sed -n '2,15p' "$0"
	exit 1
	;;
esac
