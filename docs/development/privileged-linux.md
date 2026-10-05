# Linux privileged helper service scaffold

This document describes the Linux system service scaffold for a future privileged mihomo helper. It is not an available installation path yet.

The existing `nagi service install` command continues to create a per-user `systemd --user` monitor. Its behavior and unit are unchanged. The future privileged helper uses a separate system unit, `nagi-privileged-helper.service`, at `/etc/systemd/system/nagi-privileged-helper.service`. Its foreground command is `nagi __privileged-helper`, and systemd would restart the helper after failures.

The system unit generator and removal/status code live in `internal/privileged/service`. The public Linux `Install` operation currently fails without writing a unit or calling `systemctl`. It must remain disabled until the helper enforces restricted controller access and validates effective configurations before launching root mihomo. Once enabled, installation must be invoked as root and may use only a root-owned executable in a path with no symlinks or group/world-writable components. The helper service is separate from user profile and subscription storage.

`Uninstall` requires root and stops/disables the system unit before removing it; a stop failure leaves the file in place for recovery. `Status` reports the system unit's installation and activity independently of the per-user monitor. These APIs are a backend scaffold and are not exposed as Linux CLI commands yet.
