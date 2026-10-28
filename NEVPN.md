# NE.VPN's OpenFlux

This is the OpenFlux that NE.VPN builds its exits and its apps' client from.
It exists so a fix can reach our users the day it is ready, without waiting
for an upstream release.

The code is upstream's, kept so a merge from upstream stays cheap. What we
add is small, separate, and listed below.

## Upstreams

| Remote | Repository | What it is |
|---|---|---|
| `team` | [OpenFluxTeam/OpenFlux](https://github.com/OpenFluxTeam/OpenFlux) | Where development continues since mid-October 2026 (the author, p1neappleXpress, commits there too). `main` is released (`v*` tags); `nightly` runs ahead of it. **Our base.** |
| `p1` | [p1neappleXpress/OpenFlux](https://github.com/p1neappleXpress/OpenFlux) | The original repository. Its `main` stopped at 2026-10-13; everything on it is in `team`. Watched in case it moves again. |

This repository is a GitHub fork of `p1`, so pull requests to either
upstream can be opened from here.

## Branches and tags

- `nevpn` (default): what we ship. An upstream release tag, plus the commits
  listed in [Our changes](#our-changes).
- `main`: upstream's `main`, untouched.
- `nevpn-0.13`: p1neappleXpress `v0.13.0` with the stripe, restart and direct
  takeover fixes. The fallback for exits that must keep serving 0.13 clients.
- Tags `vX.Y.Z-nevpn.N`: `X.Y.Z` is the upstream release the branch stands
  on, `N` counts our releases on it. Never move or delete a tag: the Go
  checksum database has already recorded it.

## How NE.VPN uses it

The module path stays `github.com/p1neappleXpress/OpenFlux`, so no import
changes. Every module of NE.VPN that builds OpenFlux replaces it with a tag
of this repository:

```
replace github.com/p1neappleXpress/OpenFlux => github.com/FijshH/OpenFlux v0.16.0-nevpn.1
```

The five modules (`packages/openflux-carrier`, `services/openflux-pool`,
`packages/singbox-core`, `packages/mobile-core`, `packages/desktop-core`)
move together, and the pools and the apps are released together when the
wire changes. That is the NE.VPN repository's invariant 10.

What NE.VPN builds from here: the session and its encryption, the `direct`
and `cupsonline` carriers (and `avito` when enabled), the stripe under the
room carriers, the manager, the tunnel and its L4 exit, `share` links. Not
built: the other services' transports, scripts, phpbox, the CLI, the node
installer, upstream's apps.

## Taking upstream changes

1. `nevpn/upstream-review.sh` lists every upstream commit that is neither
   in `nevpn` nor decided in [`nevpn/ledger.tsv`](nevpn/ledger.tsv). The
   `upstream-watch` workflow runs it daily and keeps one issue up to date.
2. For each commit: take it with `git cherry-pick -x` (the trailer records
   it), or add a line to the ledger with `skip` or `later` and why.
3. A new upstream release: start a branch at its tag, replay our changes
   (`git cherry-pick -x` of each line in the table below), run CI, then
   move `nevpn` to it and tag `vX.Y.Z-nevpn.1`.
4. Before tagging, check the wire: a change to the handshake, encryption,
   framing, the stripe or a carrier's messages means the pools and the
   apps go out together.

## Our changes

Every commit on `nevpn` that is not on the upstream tag it stands on.

| Commit | Source | Why |
|---|---|---|
| l3: return path honours the carrier's limit and cuts what offload merged | `team/nightly` 0842c4d | Bulk downloads through an exit fell to 5-10 KB/s when receive offload merged segments larger than the carrier's limit. |
| transport: a carrier can declare a packet size below 1280, and a batch size that is a hard limit | `team/nightly` f30ec4b | Needed by 0842c4d and fe77d0e. |
| transport: control frames never wait on a carrier that takes nothing | `team/nightly` 109566c | A link nobody dials filled its batcher with pings and stopped the receive path: no new client could finish a handshake until a restart. |
| tunnel: packets from the device honour the carrier's packet size | `team/nightly` fe77d0e | A device's TUN MTU above the carrier's: the first segments of a connection never arrived. |
| nevpn: this file, the ledger, CI and the upstream watch | ours | Keeping the fork. |
| session: the client survives a node restart from its first seconds | [OpenFluxTeam#4](https://github.com/OpenFluxTeam/OpenFlux/pull/4) | A client stuck on an exit that restarted (a pool deploy) until it noticed by itself. |
| transport: the master key in 4 MiB, not 32 | [OpenFluxTeam#5](https://github.com/OpenFluxTeam/OpenFlux/pull/5) | The same scrypt key bytes with an eighth of the memory: phones kill a VPN service that spikes. About twice the CPU per key on an exit, once per secret. |
| ios: eager KDF, smaller zstd window; transport: drop EagerKDF, keep the 1 MB zstd window | [OpenFluxTeam#7](https://github.com/OpenFluxTeam/OpenFlux/pull/7) | An encoder takes 1.3 MB instead of 16; any decoder reads it, so the wire is unchanged. The pool runs one per session. |
| stripe: release a stopped stripe | [p1neappleXpress#196](https://github.com/p1neappleXpress/OpenFlux/pull/196) | Every stopped stripe kept its delivery goroutine and rings (about 5 MiB): the pool and the phone leaked one per reconnect. |

The `upstream-watch` issue and `nevpn/ledger.tsv` hold everything not taken.
