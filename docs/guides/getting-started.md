# Getting started

This guide starts and controls a local mihomo instance with Nagi.

1. Build Nagi and the pinned mihomo commit as described in [Build and test](../development/build-and-test.md).
2. Run `./bin/nagi start`. On first use, Nagi creates a minimal `default` profile with a local mixed proxy port at `17890`.
3. Run `./bin/nagi status` to check the PID and mihomo version. Run `./bin/nagi logs` if startup fails.
4. Run `./bin/nagi proxy groups` to list configured groups, and `./bin/nagi proxy select GROUP NODE` to change a selection.
5. Run `./bin/nagi stop` when finished.

Nagi stores a profile at the platform specific configuration path. Use `nagi config show` to inspect it and `nagi config validate` to check it. To add or replace a profile with validation and atomic replacement, use `nagi profile import NAME FILE`. Switch with `nagi profile list` and `nagi profile use NAME`. The complete command and path definitions are in the [CLI reference](../reference/cli.md) and [runtime design](../design/runtime.md).

Use `nagi --json COMMAND` for scripts and the macOS app. Subscription URLs are stored locally and omitted from list output. `subscription update` downloads content into Nagi's cache; it does not automatically activate the downloaded content as a mihomo profile or provider.
