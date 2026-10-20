# Release process

This document defines the versioning and release rules for Nagi and the GitHub
Release assets consumed by `scripts/install.sh`. `make release` builds the CLI
and mihomo archives for Linux and macOS. If `bin/Nagi.app` exists, the release
script includes it in the archive matching the macOS host architecture.

Before preparing a release with engine changes, complete the [engine maintenance workflow](engine-maintenance.md), including remote `main` verification and a locked build. Publishing engine source alone does not publish Nagi installation artifacts.

## Version numbers

Published Nagi releases use a project-specific versioning convention with a
leading `v` in the Git tag. This convention does not strictly follow Semantic
Versioning: a minor release may include a localized incompatible CLI change.

```text
vMAJOR.MINOR.PATCH
```

- Increment **MAJOR** for major architecture or configuration-storage changes,
  or broad interface or supported-platform incompatibilities that require an
  overall migration.
- Increment **MINOR** for new functionality, commands, options, integrations,
  or localized CLI adjustments. A localized incompatible CLI change requires
  explicit compatibility and migration notes, but does not by itself require
  a major release.
- Increment **PATCH** for backwards-compatible fixes, documentation corrections,
  and dependency or build updates that do not change the public interface.

Classify changes by their scope and migration impact. For example, simplifying
`nagi rules import NAME FORMAT SOURCE [TARGET]` to `nagi rules import NAME SOURCE`
is a minor-release change when existing saved imports remain usable. Its release
notes must explain the removed arguments, the supported native-rule input, and
how to update existing commands and input files. A minor version does not promise
compatibility for every CLI invocation; users maintaining scripts should review
the release notes before upgrading.

Pre-releases use a hyphenated identifier, for example `v1.4.0-rc.1` or
`v1.4.0-beta.2`. They are ordered before the corresponding final release and
must not be presented as stable releases. Do not reuse a version number after a
tag has been published; create a new patch or pre-release identifier instead.

The release tag is the source of the version embedded in the CLI. The build
script uses `git describe --tags --always --dirty`; therefore a release build
must be made from a clean checkout at the release tag. A dirty checkout or a
build from an untagged commit is not a publishable release build. Verify the
embedded value with:

```sh
./bin/nagi --json version
```

The `nagi` field must equal the release tag. The `mihomo_commit` field must
match `engine.lock`.

The App packaging script uses the same `git describe --tags --always --dirty`
value for `CFBundleShortVersionString`, removing the leading `v`. A clean build
at `v1.0.1`, for example, displays `Version 1.0.1` in About Nagi.
`CFBundleVersion` is the number of commits reachable from `HEAD`.
Development builds retain the commit suffix or `dirty` marker rather than
claiming to be the tagged release. These unsigned development bundles are not
intended for App Store submission.

The release workflow verifies a clean tagged checkout, runs the host CLI to
check its release version and locked engine commit, and checks the App's
`CFBundleShortVersionString` inside the uploaded macOS archive. A missing App
or version mismatch fails publication. Local build scripts do not enforce all
release policy, so the remaining checklist still requires review.

Its test step runs `make test` with `GOFLAGS=-p=1`, so Go test packages run
serially. This reduces competition with the bounded script throughput fixture;
it does not skip tests or change production deadlines. See the
[diagnostic comparison](build-and-test.md) for the measured results.

## Preparing a release

1. Confirm that the working tree is clean and that the intended version follows
   the rules above.
2. Review changes since the previous release. Identify the previous release
   tag and inspect the complete range up to the candidate tag:

   ```sh
   git tag --list 'v*' --sort=-version:refname | head
   git log --oneline PREVIOUS_TAG..HEAD
   git diff --stat PREVIOUS_TAG..HEAD
   ```

   The review must cover commits, user-visible behavior, CLI and JSON interface
   changes, compatibility notes, security fixes, and build or dependency
   changes. If this is the first release, review the complete history instead.
3. Update any authoritative documentation affected by the changes. Run the
   required checks from [Build and test](build-and-test.md), including `make
   test` and a locked `./scripts/build.sh`.
4. Create and push the version tag only after the review and checks pass:

   ```sh
   git tag -a vX.Y.Z -m "Nagi vX.Y.Z"
   git push origin vX.Y.Z
   ```

5. Check out the tag in a clean workspace. On a macOS release host, run
   `make build app` first when the release should offer the optional App. Then
   run `make release`. Inspect every target under `dist/`, including the four
   `nagi-*.tar.gz` archives and `SHA256SUMS`, and verify executable permissions.
   Run the version check above for targets that can execute on the release host;
   for other targets, verify the embedded version from the build output or with
   a binary inspection tool.
6. The pushed tag triggers the [`publish release`](../../.github/workflows/release.yml)
   workflow. It builds the archives, verifies `SHA256SUMS`, generates release
   notes, and creates the GitHub Release with the tagged artifacts. Review the
   created release before announcing it.

## Release notes requirement

Every release description must explicitly document the changes from the
previous release to the current release. Do not summarize only the latest
commit or rely on a link to a comparison page. Use the previous tag as the
lower bound, include the current tag as the upper bound, and state the range in
the notes.

Use this structure:

```markdown
# Nagi vX.Y.Z

Changes since [vPREVIOUS]:
`vPREVIOUS..vX.Y.Z`

## Added
- ...

## Changed
- ...

## Fixed
- ...

## Compatibility and upgrade notes
- ...

## Artifacts
- ...
```

Remove empty sections, but keep the comparison range and include all
user-visible changes. Mention breaking changes and required migration steps in
**Compatibility and upgrade notes**. If there were no entries in a category,
omit that category rather than implying that it was reviewed incompletely.
For localized incompatible CLI changes in a minor release, identify the affected
commands and removed arguments or input formats, show the replacement usage,
and explain any changes required to scripts or input files and the treatment of
existing saved data.

## Request rewrite release gate

The implementing engine source is published at the commit in `engine.lock` and
verified obtainable from a fresh fetch of the declared repository/ref. The
matching locked source build is verified in the acceptance record. Nagi release
binaries have not been published for this change: do not advertise rewrite
activation through ordinary installation or `nagi update` until a matching Nagi
release exists. For subsequent pin updates, publish the implementing engine commit,
verify a fresh fetch, update `engine.lock` and run the matching locked build.
The [request rewrite verification record](request-rewrite-acceptance.md) also
requires native macOS Swift build/tests, packaging, trust/UI acceptance and real
TUN/backend evidence before claiming those platforms accepted.

## Final checklist

- [ ] Version is a new `vMAJOR.MINOR.PATCH` or explicitly identified pre-release.
- [ ] Working tree is clean and the tag points to the reviewed commit.
- [ ] Changes from the previous release tag were reviewed and listed in the
      release description.
- [ ] `make test` passed.
- [ ] Locked `./scripts/build.sh` passed.
- [ ] `make release` artifacts were inspected.
- [ ] `dist/SHA256SUMS` matches every uploaded archive.
- [ ] GitHub Release assets use the fixed names expected by the online installer.
- [ ] The tag-triggered publish workflow completed and the created release was reviewed.
- [ ] `./bin/nagi --json version` reports the release tag and locked mihomo
      commit.
- [ ] The packaged App's `CFBundleShortVersionString` equals the release tag
      without its leading `v`.
- [ ] Release notes include compatibility or migration information where
      applicable.
