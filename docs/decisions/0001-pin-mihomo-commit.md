# 0001: Pin the mihomo source commit

Status: accepted

## Context

Nagi builds and launches a separately maintained mihomo executable. The CLI and macOS app need to agree with the mihomo control API used during testing. A moving branch reference alone cannot identify the source used by a release.

## Alternatives

- Follow the latest branch head at build time. This is simple but makes builds change without a Nagi source change.
- Copy mihomo source into Nagi. This duplicates the core repository and blurs ownership.
- Record a repository, branch ref, and exact commit in `engine.lock`.

## Decision

Nagi records an exact mihomo commit in `engine.lock`. The build accepts a local checkout only at that commit, or fetches the declared ref and verifies the commit is reachable from it. The available fork's `main` branch is the current ref. A dedicated customization branch may replace it in a future lock update.

## Consequences

Builds are reproducible with respect to mihomo source, and source updates require an explicit lock change. Nagi's release process must build and distribute the corresponding mihomo executable. Core customizations remain in the mihomo repository.
