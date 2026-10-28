#!/usr/bin/env bash
# The tests of what NE.VPN builds from OpenFlux, with the race detector, and
# a build of everything. nevpn-ci runs this; run it before every tag.
#
#   nevpn/test.sh          vet, race tests, build
#   nevpn/test.sh -quick   the same without -race (a few times faster)
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

PKGS=(./transport/ ./transport/cupsonline/ ./transport/control/ ./transport/manager/
	./transport/registry/ ./tunnel/... ./share/ ./utils/ ./netbind/)
race=(-race)
[ "${1:-}" = "-quick" ] && race=()

go vet "${PKGS[@]}" ./nevpn/...
go test ${race[@]+"${race[@]}"} -count=1 -timeout 15m "${PKGS[@]}"
go build ./...
echo "nevpn/test.sh: ok"
