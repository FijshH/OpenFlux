#!/usr/bin/env bash
# Tags origin/nevpn as the next release: vX.Y.Z-nevpn.N, X.Y.Z being the
# OpenFluxTeam release it stands on and N one more than the last of ours on
# it. Refuses unless nevpn-ci passed on that commit. A tag is never moved or
# deleted: the Go checksum database records it.
#
#   nevpn/tag.sh            tag and push
#   nevpn/tag.sh -n         say what it would tag
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

# upstream_base COMMIT: the newest OpenFluxTeam release tag (refs/tags/team/vX.Y.Z)
# that COMMIT stands on.
upstream_base() {
	local tag
	for tag in $(git for-each-ref --format='%(refname:strip=3)' 'refs/tags/team/v*' |
		grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -rV); do
		if git merge-base --is-ancestor "refs/tags/team/${tag}" "$1"; then
			echo "$tag"
			return
		fi
	done
	echo "no OpenFluxTeam release under $1" >&2
	return 1
}

REPO="${NEVPN_FORK:-FijshH/OpenFlux}"

git fetch -q --no-tags team '+refs/tags/v*:refs/tags/team/v*' 2>/dev/null || true
git fetch -q --tags origin nevpn
head="$(git rev-parse origin/nevpn)"
base="$(upstream_base "$head")"
last="$(git tag -l "${base}-nevpn.*" | sed "s/^${base}-nevpn\.//" | sort -n | tail -1)"
tag="${base}-nevpn.$(( ${last:-0} + 1 ))"
if existing="$(git tag --points-at "$head" -l 'v*-nevpn.*')" && [ -n "$existing" ]; then
	echo "origin/nevpn (${head:0:9}) is already ${existing}."
	exit 0
fi

conclusion="$(gh run list -R "$REPO" --workflow nevpn-ci.yml --commit "$head" --json conclusion,status \
	--jq 'map(select(.status=="completed"))[0].conclusion // "none"')"
echo "origin/nevpn ${head:0:9}, on ${base}: next tag ${tag}, nevpn-ci ${conclusion}."
[ "$conclusion" = "success" ] || { echo "Tag only a commit nevpn-ci passed."; exit 1; }
[ "${1:-}" = "-n" ] && exit 0

git tag -a "$tag" "$head" -m "NE.VPN OpenFlux ${tag}: OpenFluxTeam ${base} plus the commits in NEVPN.md"
git push -q origin "refs/tags/${tag}"
echo "Pushed ${tag}. In the vpn repository: scripts/openflux_bump.sh ${tag}"
