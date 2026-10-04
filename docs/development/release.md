# Release process

This document defines the versioning and release rules for Nagi. It covers the
CLI and mihomo binaries produced by `make release`; the macOS SwiftUI app is not
packaged by the current release script.

## Version numbers

Published Nagi releases use [Semantic Versioning](https://semver.org/) with a
leading `v` in the Git tag:

```text
vMAJOR.MINOR.PATCH
```

- Increment **MAJOR** for an incompatible CLI, JSON interface, configuration,
  or supported-platform change.
- Increment **MINOR** for backwards-compatible functionality or a new command,
  option, or supported integration.
- Increment **PATCH** for backwards-compatible fixes, documentation corrections,
  and dependency or build updates that do not change the public interface.

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

These version rules are release policy. The current scripts do not reject an
invalid tag, dirty checkout, or untagged build, so the release checklist must be
completed manually.

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
   test` and a locked `make build`.
4. Create and push the version tag only after the review and checks pass:

   ```sh
   git tag -a vX.Y.Z -m "Nagi vX.Y.Z"
   git push origin vX.Y.Z
   ```

5. Check out the tag in a clean workspace, run `make build`, and then run
   `make release`. Inspect every target under `dist/` and verify executable
   permissions. Run the version check above for targets that can execute on the
   release host; for other targets, verify the embedded version from the build
   output or with a binary inspection tool.
6. Publish the release using the release notes prepared in the next section and
   attach only artifacts produced from the tagged commit. The current repository
   does not automate Git hosting publication.

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

## Final checklist

- [ ] Version is a new `vMAJOR.MINOR.PATCH` or explicitly identified pre-release.
- [ ] Working tree is clean and the tag points to the reviewed commit.
- [ ] Changes from the previous release tag were reviewed and listed in the
      release description.
- [ ] `make test` passed.
- [ ] Locked `make build` passed.
- [ ] `make release` artifacts were inspected.
- [ ] `./bin/nagi --json version` reports the release tag and locked mihomo
      commit.
- [ ] Release notes include compatibility or migration information where
      applicable.
