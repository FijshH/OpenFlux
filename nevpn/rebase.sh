#!/usr/bin/env bash
# Moves NE.VPN's changes onto a new OpenFluxTeam release.
#
#   nevpn/rebase.sh v0.17.0
#
# Makes branch nevpn-v0.17.0 at OpenFluxTeam's tag and replays on it every
# commit of nevpn that is not upstream's: the commits since the upstream tag
# nevpn stands on, minus those whose change the new release already has
# (listed as "upstream has it"). A conflict stops the replay: resolve it,
# `git cherry-pick --continue`, and run this again with --resume to see what
# is next. Nothing is pushed. Then (NEVPN.md, "A new upstream release"):
# nevpn/test.sh, update NEVPN.md's table, push the branch over nevpn, tag.
#
# Upstream tags are kept under refs/tags/team/ (p1neappleXpress and
# OpenFluxTeam reuse version numbers for different commits; ours are
# vX.Y.Z-nevpn.N).
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

if [ "${1:-}" = "--resume" ]; then
	if [ -d .git/sequencer ] || [ -f "$(git rev-parse --git-path CHERRY_PICK_HEAD)" ]; then
		echo "A cherry-pick is still in progress: resolve, git add, git cherry-pick --continue."
		exit 1
	fi
	echo "Replay done on $(git branch --show-current). Next: nevpn/test.sh, NEVPN.md, push, tag."
	exit 0
fi

new="${1:?usage: nevpn/rebase.sh vX.Y.Z (an OpenFluxTeam release tag)}"
[ -z "$(git status --porcelain --untracked-files=no)" ] || { echo "The work tree has changes; commit or set them aside first."; exit 1; }

git remote get-url team >/dev/null 2>&1 || git remote add team https://github.com/OpenFluxTeam/OpenFlux.git
git fetch -q --no-tags team '+refs/tags/v*:refs/tags/team/v*' main nightly
git fetch -q origin nevpn
git rev-parse -q --verify "refs/tags/team/${new}" >/dev/null || { echo "OpenFluxTeam has no tag ${new}"; exit 1; }

old="$(git describe --tags --abbrev=0 --match 'team/v[0-9]*' origin/nevpn)"
echo "nevpn stands on ${old#team/}; moving to ${new}."
[ "${old#team/}" != "${new}" ] || { echo "Already there."; exit 0; }

replay=()
while read -r mark sha; do
	subject="$(git log -1 --format=%s "$sha")"
	if [ "$mark" = "-" ]; then
		echo "  upstream has it: ${sha:0:9} ${subject}"
	else
		echo "  replay:          ${sha:0:9} ${subject}"
		replay+=("$sha")
	fi
done < <(git cherry "refs/tags/team/${new}" origin/nevpn "${old}")

branch="nevpn-${new}"
git checkout -q -B "$branch" "refs/tags/team/${new}"
echo "On ${branch}. Replaying ${#replay[@]} commits."
# No -x: the commits keep their messages, upstream ones their original
# "cherry picked from" line. A change upstream now makes as well comes out
# empty and is dropped.
if [ "${#replay[@]}" -gt 0 ] && ! git cherry-pick --empty=drop "${replay[@]}"; then
	echo
	echo "Stopped on a conflict. Resolve it, git add, git cherry-pick --continue; then nevpn/rebase.sh --resume."
	exit 1
fi
echo
echo "Done. Next: nevpn/test.sh; remove from NEVPN.md the commits upstream now has (above);"
echo "nevpn/upstream-review.sh; then git push --force-with-lease origin ${branch}:nevpn and nevpn/tag.sh."
