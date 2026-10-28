#!/usr/bin/env bash
# Lists the upstream commits nobody has decided on yet: on an upstream ref,
# not in this branch (by patch, or by a "cherry picked from" line), and not in
# nevpn/ledger.tsv. Prints Markdown; exit status 0 either way.
#
#   nevpn/upstream-review.sh            # fetches the upstreams first
#   NO_FETCH=1 nevpn/upstream-review.sh # uses what is already fetched
#
# The upstreams and their refs are in UPSTREAMS below. A commit that touches
# only paths we never build (OURS_RE does not match) is listed apart, in one
# line each, so the ones that matter stand out.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
BASE="${BASE:-HEAD}"
LEDGER=nevpn/ledger.tsv

# remote name, URL, refs to watch
UPSTREAMS=(
	"team https://github.com/OpenFluxTeam/OpenFlux.git main nightly"
	"p1 https://github.com/p1neappleXpress/OpenFlux.git main"
)

# What NE.VPN builds: the session, its carriers, the tunnel, links, and
# the module itself. Everything else (other services' transports, apps,
# node installer, phpbox, scripts) is listed but not flagged.
OURS_RE='^(go\.mod|go\.sum|transport/[^/]+\.go|transport/(cupsonline|control|manager|registry|ipc)/|tunnel/|share/|utils/|netbind/|network/)'

for u in "${UPSTREAMS[@]}"; do
	set -- $u
	name=$1 url=$2
	shift 2
	git remote get-url "$name" >/dev/null 2>&1 || git remote add "$name" "$url"
	if [ -z "${NO_FETCH:-}" ]; then
		git fetch -q --no-tags "$name" "$@"
	fi
done

decided="$(mktemp)"
trap 'rm -f "$decided"' EXIT
{
	cut -f1 "$LEDGER" | grep -E '^[0-9a-f]{7,40}$' || true
	git log --format=%B "$BASE" | sed -n 's/^(cherry picked from commit \([0-9a-f]*\))$/\1/p'
} | cut -c1-12 | sort -u >"$decided"

total=0
echo "# Upstream commits to review"
echo
echo "Base: \`$(git rev-parse --short=12 "$BASE")\` ($(git log -1 --format=%cs "$BASE")). Decide each one in \`nevpn/ledger.tsv\` (take, skip, later) or take it with \`git cherry-pick -x\`."
for u in "${UPSTREAMS[@]}"; do
	set -- $u
	name=$1
	shift 2
	for ref in "$@"; do
		ours="" other=""
		while read -r mark sha subject; do
			[ "$mark" = "+" ] || continue
			short="${sha:0:12}"
			grep -qx "$short" "$decided" && continue
			[ "$(git rev-list --parents -n1 "$sha" | wc -w)" -gt 2 ] && continue # merges
			files="$(git diff-tree --no-commit-id --name-only -r "$sha")"
			line="- [\`${short}\`](https://github.com/$(git remote get-url "$name" | sed 's|https://github.com/||;s|\.git$||')/commit/${sha}) $(git log -1 --format=%cs "$sha") ${subject}"
			if grep -qE "$OURS_RE" <<<"$files"; then
				ours+="$line"$'\n'
			else
				other+="$line"$'\n'
			fi
			total=$((total + 1))
		done < <(git cherry -v "$BASE" "$name/$ref" | awk '{print $1, $2, substr($0, index($0,$3))}')
		if [ -n "$ours" ] || [ -n "$other" ]; then
			echo
			echo "## $name/$ref"
			if [ -n "$ours" ]; then
				echo
				echo "Touch what we build:"
				echo
				printf '%s' "$ours"
			fi
			if [ -n "$other" ]; then
				echo
				echo "Other parts only:"
				echo
				printf '%s' "$other"
			fi
		fi
	done
done
echo
echo "Undecided: ${total}."
