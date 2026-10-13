# Per-profile persistent routing rules

Status: accepted

This decision scopes Nagi-owned custom rules, imported full-rule bundles, and stored provider sets to one profile. Mihomo continues to own rule syntax, dependency validation, matching, and traffic routing.

## Context

A rule can target a proxy group or node that exists only in one subscription profile. Applying a global rule overlay to every profile makes an unrelated profile invalid when that target is absent. In particular, a node-less `default` profile must remain usable after rules are imported into a subscription profile. Subscription candidate validation must inspect the rules owned by the candidate profile.

## Alternatives

- Retain one global rules file and require every target to exist in every profile. This couples independent subscriptions and prevents safe switching to `default`.
- Filter global rules by currently available targets. This silently changes user routing intent when targets disappear and makes the same stored rule behave differently across profiles.
- Store rules per profile and select the matching rules document during effective generation and candidate validation.

## Decision

Store each profile's rules separately under `config/rules/PROFILE.yaml`. Stored-rule commands operate on the selected profile. Effective generation and subscription validation use the explicitly named profile, including when a subscription application changes the selection. Rule names are unique within a profile; different profiles can reuse names. Subscription updates retain the target profile's rules, and profile removal cleans up its rules file transactionally.

Do not read or migrate the former global `config/rules.yaml`. Its ownership cannot be inferred safely from targets because profiles can share group names and imports may contain rules with several targets. Users explicitly import wanted rules into each intended profile.

## Consequences

Profiles can switch independently and `default` does not inherit subscription-specific routing. Missing targets still reject the owning profile's candidate configuration instead of silently dropping rules. Users who want the same rules on several profiles import them separately; changes remain independent.

The CLI owns persistence, configuration composition, activation, and rollback. The UI invokes those operations through the CLI. The authoritative command and storage contract is in the [rules reference](../reference/cli.md#rules). See the [rules guide](../guides/rules.md) for import and selection steps and the [runtime design](../design/runtime.md#rules-and-subscription-persistence) for composition and recovery.
