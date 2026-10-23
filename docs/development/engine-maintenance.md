# Engine maintenance

This document defines maintenance of the long-lived `ronigooja/mihomo` fork,
integration of engine changes, manual upstream synchronization, and verified
updates to Nagi's `engine.lock`. Nagi release packaging is defined in the
[release process](release.md).

## Branch ownership

The fork's `main` integrates Nagi's engine customizations. Develop and review
changes on feature branches, then integrate them into `main` before updating
Nagi's normal source pin. A published feature branch is an intermediate state;
it does not complete mainline integration. Feature branches need not be deleted
when integrated.

Keep proxy protocols, DNS, forwarding, routing and TUN implementation in mihomo.
Keep configuration, persistence, process management and user operations in Nagi.
The [architecture](../../ARCHITECTURE.md) defines these boundaries.

`engine.lock` records the fork URL, `ref: main`, and an exact commit. Later
updates to `main` do not automatically change Nagi builds. Do not rewrite
published `main` history or force-push it to integrate a feature or upstream
update. There is no separate `nagi/meta` maintenance branch or automated upstream
synchronization in the current workflow.

## Integrate a feature

1. Start from a clean fork checkout. Fetch `origin`, inspect the feature diff
   against `origin/main`, and review protocol, configuration and security
   compatibility with Nagi. Run relevant engine regression tests and build with
   `with_gvisor`; use [Build and test](build-and-test.md) for Nagi checks and
   feature-specific acceptance requirements.
2. Integrate the reviewed feature into local `main`. Use a fast-forward when
   possible. When histories diverge, merge and resolve conflicts on an
   integration branch, review the resolution and rerun affected checks before
   advancing `main`. Existing history includes both fast-forwards and selected
   cherry-picks; do not replay changes already present in `main`.
3. Push `main` to `origin` without force. If the remote advanced, fetch and
   integrate its changes, then repeat the affected checks. A local merge alone
   does not publish the integration.
4. Verify the integrated commit from a fresh fetch of the remote `main`, then
   update Nagi as described below. If publication or verification fails, leave
   Nagi's existing pin in place.

For a reviewed feature branch that can fast-forward, run from the fork checkout
(replace `feature/example` with the actual reviewed branch):

```sh
git fetch origin
git switch main
git merge --ff-only origin/main
git merge --ff-only feature/example
git push origin main:main
git ls-remote origin refs/heads/main
```

## Synchronize upstream

Upstream synchronization is a separate, manual integration task. Review upstream
changes regularly and before engine updates that need upstream fixes. Updating
Nagi's lock alone does not synchronize upstream.

Use `https://github.com/MetaCubeX/mihomo.git` as an `upstream` remote. Check an
existing remote's URL before using it; add it only when absent. Select and record
the upstream branch and exact commit intentionally. Do not infer the desired
development branch from the remote's default branch: on 2026-10-19 the upstream
default was `main`, while `Meta` also existed.

Fetch the selected branch and review its changes relative to the fork, including
dependencies, configuration fields and management APIs. Create an integration
branch from `origin/main` and merge the selected upstream commit into it. Preserve
Nagi's custom engine interfaces and resolve conflicts there. Record the upstream
branch and commit in the integration commit or review description.

Run upstream and custom regression tests relevant to the merge, build with
`with_gvisor`, and verify Nagi configuration generation, API compatibility,
persistence and activation where affected. Runtime or platform changes require
their relevant integration checks. Document failed or unavailable checks and
resolve release-blocking failures before advancing the pin. The existing test
limitations in [Build and test](build-and-test.md) and feature acceptance records
still apply; do not treat a push as proof that CI ran.

After review, advance and publish the fork's `main` without rewriting history,
then follow the same remote verification and pin update steps as a feature.
If integration fails, retain the previous published mainline and Nagi pin until
the candidate is corrected.

## Verify publication and update Nagi

From Nagi, verify the intended commit is reachable using a fresh temporary
repository. This development example uses the current fork URL and locked
commit; after an engine change, substitute the reviewed integrated commit:

```sh
engine_verify_dir=$(mktemp -d)
git -C "$engine_verify_dir" init -q
git -C "$engine_verify_dir" fetch https://github.com/ronigooja/mihomo.git refs/heads/main
engine_commit=a37bfbb72970697a53e9cf549ff3f10006369e49
git -C "$engine_verify_dir" merge-base --is-ancestor "$engine_commit" FETCH_HEAD
rm -rf -- "$engine_verify_dir"
```

Run these commands in order and stop on failure. Fetch and ancestry checks must
exit zero. Remove the temporary repository after inspecting any failure too.
An integrated commit may remain a valid pin even if remote `main` later advances.

Once verified, set `engine.lock` to the published repository, `ref: main`, and
the exact integrated commit. Run `./scripts/build.sh` with `MIHOMO_DIR` unset so
the normal remote-fetch path checks reachability and builds the locked engine
and CLI. Run `./bin/nagi --json version` and verify `mihomo_commit` equals the
lock. Use [Build and test](build-and-test.md) to select an installed Go toolchain
and the checks appropriate to the changed capabilities.

Update affected authoritative interface, design and verification documents in
the same Nagi change. Keep historical acceptance evidence explicit about the
branch and commit actually tested; moving an unchanged commit to `main` does
not establish new platform acceptance. Check documentation links and examples.

Engine source publication, Nagi source pinning and Nagi binary releases are
separate steps. Installation and `nagi update` obtain the new engine only after
a matching Nagi release is built and published through the release process.
