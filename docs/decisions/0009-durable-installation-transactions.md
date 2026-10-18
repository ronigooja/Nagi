# Durable installation file transactions

Status: accepted

This decision covers filesystem changes made by artifact installation, updates,
whole removal, and App-only maintenance. Runtime and OS service-manager recovery
remain separate responsibilities.

## Context

An EXIT trap can undo a reported shell failure, but cannot run after abrupt
process death or power loss. Keeping rollback state only in shell variables also
loses the operation list when the process exits. The ordinary installation record
can be missing during replacement or removal, so it cannot locate or authorize
the recovery procedure on its own. Destination files, App bundles, and the user's
default profile can live on different filesystems.

## Alternatives

- Continue shell-only rollback. This handles ordinary errors but cannot recover
  interrupted operations or enforce durable ordering.
- Replace one installation directory by rename. This cannot include the separate
  App and profile destinations or guarantee consistency across filesystems.
- Add a separate installer executable. This provides filesystem primitives but
  adds another platform artifact and bootstrap/version compatibility contract.
- Use a private entry point in the existing CLI for a durable journal and file
  operations, while retaining shell orchestration.

## Decision

Use the private `__install-transaction` CLI entry point backed by
`internal/installtx`. Dispatch it before loading runtime configuration. Keep an
independent journal at `PREFIX/.nagi-transaction` and serialize mutating installer
operations with a per-prefix filesystem lock. Preserve original and candidate
contents before publishing the prepared manifest. Synchronize files and
relevant directories before acknowledging durable transitions; use macOS
`F_FULLFSYNC` for regular files and Linux `fsync`.

Recovery restores original files until a durable commit marker exists, then
retains the committed result. It does not consume backups during rollback.
Cleanup retires the entire journal by rename before deleting its contents so a
cleanup interruption cannot erase only the commit marker. Corrupt recovery data
and unsafe paths stop recovery and retain evidence rather than guessing which
version should survive.

The shell installer recovers before interpreting its installation record.
Installed CLI maintenance also detects pending recovery before reading that
record. Runtime entry points that could activate an incomplete installation
refuse to start it until explicit maintenance recovery completes. Read-only
verification remains read-only. A verified release's runnable CLI and installer
provide recovery when the installed executable is missing.

## Consequences

The existing CLI release artifact supplies platform filesystem durability
primitives without another shipped executable. The shell remains responsible for
managed-path validation, helper lifecycle, and operation sequencing; the CLI
continues to own engine and user service lifecycle.

Backups increase disk use and write volume, and some unsupported filesystems may
reject required synchronization. Such errors must not be silently ignored.
Multiple destinations are not visible atomically to concurrent readers; recovery
establishes the complete old or committed result. Existing binaries keep their
installed path during replacement, preserving Linux executable-relative companion
lookup for a running terminal menu.

A file journal does not make launchd/systemd state or runtime engine processes
transactional. Hard-crash recovery does not automatically resume them. Tests
cover process termination, synchronization failure, and repeated recovery, but
physical power-loss durability additionally requires the separate
[VM power-cut acceptance](../development/install.md#durability-verification).
