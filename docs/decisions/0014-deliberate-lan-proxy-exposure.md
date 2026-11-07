# 0014 Deliberate LAN proxy exposure with privileged TUN

Status: accepted

Scope: the accepted coordinated mihomo/helper implementation target for simultaneous privileged TUN and top-level proxy sharing. Engine pin integration, CI, and runtime acceptance are pending; this decision is not evidence that the currently pinned engine supports the target.

## Context

The root helper provides TUN permissions without giving the user direct access to a root controller. Existing restricted configuration validation prevents non-loopback proxy exposure, so runtime LAN commands alone cannot provide simultaneous privileged TUN and LAN sharing. Requiring a user-backend switch would sacrifice the helper's TUN/DNS permissions and conflate proxy sharing with process privilege selection.

The intended user action is to share ordinary proxy listeners deliberately, not to expose management APIs, DNS, or arbitrary inbound protocols. Mihomo owns listener semantics and native restricted-policy enforcement; Nagi owns authenticated helper operations and configuration/lifecycle coordination.

## Alternatives

1. Keep privileged LAN prohibited and require a manual user-backend switch. This preserves the old boundary but does not satisfy simultaneous privileged TUN and LAN access.
2. Disable restricted policy or broadly allow inbound listeners. This expands root network exposure and unrelated configuration authority beyond the request.
3. Coordinate a narrow proxy-exposure allowance in mihomo and the helper while preserving all other restricted boundaries.

## Decision

Choose option 3. Retain `--config-policy restricted` for validation and the running root engine. Permit deliberate LAN exposure only through top-level HTTP, SOCKS, and mixed proxy settings; retain the controller's private transport and the existing DNS and explicit `listeners` restrictions. Apply matching validation at startup, configuration reload, and runtime patch boundaries. The [CLI reference](../reference/cli.md#lan-exposure-boundary) is authoritative for allowed ports, binds, command syntax, and security warnings.

LAN operations use the selected backend and existing controller mediation. They neither switch backends nor alter TUN intent. Keep enable/disable runtime-only, with existing reload/restart reset behavior; do not introduce a saved LAN preference as part of this change.

## Consequences

Users can intentionally share ordinary proxy listeners while retaining privileged TUN once the coordinated implementation is integrated and verified. This is a deliberate expansion of root proxy-listener exposure, not a general relaxation of root configuration policy. Wildcards are not LAN-only isolation, loopback cannot share with other devices, and enabling access supplies neither authentication nor firewall protection. Users remain responsible for reachable-interface selection and access controls.

The engine and helper must ship together with an updated engine pin. Until that integration and its evidence exist, guides and references must identify simultaneous privileged TUN/LAN as pending rather than a released or verified capability. Existing clients retain their CLI/JSON interface and runtime-only LAN behavior.

## Verification gates

When integrating this target, use GitHub-hosted Linux and macOS runners for the matching builds and tests. Cover restricted-policy startup and reload plus helper PATCH acceptance for boolean LAN state, literal IPv4/IPv6 binds, wildcard binds, legacy configuration `localhost`, disabled ports, and the allowed high-port range. Reject invalid types, arbitrary hostnames, low/out-of-range ports, and unchanged forbidden controller, DNS, and explicit-listener exposure cases. Verify CLI syntax independently: explicit ADDRESS remains literal-IP-only.

On a GitHub-hosted macOS runner, verify the actual helper/root engine retains restricted policy, an up TUN adapter, and managed DNS while LAN is enabled, then returns to loopback on disable without changing backend or TUN intent. Check reload/restart restores profile LAN values and that rejected changes report failure without claiming success. Record configured state, actual listener observations, and any separate-device reachability checks separately; runner-local checks cannot establish reachability from another LAN device. No build, test, or runtime acceptance evidence is asserted by this documentation change.
