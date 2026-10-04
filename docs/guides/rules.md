# Manage routing rules

Use the CLI to inspect mihomo's active order and keep local rules separate from subscription profiles.

```sh
nagi rules list
nagi rules providers
nagi rules add local-direct DOMAIN-SUFFIX intranet.example DIRECT
nagi rules import-local ads domain ./ads.yaml REJECT
nagi rules conflicts
nagi rules connection CONNECTION_ID
```

Custom rules are evaluated before profile and subscription rules. Disable a rule temporarily with `nagi rules disable NAME`; remove it with `nagi rules remove NAME`. Imported remote sets are downloaded and stored as snapshots with `nagi rules import-remote NAME BEHAVIOR URL TARGET`.

Use `nagi rules conflicts` to find duplicate matchers and rules placed after `MATCH`. The report cannot infer every semantic conflict in regular expressions or provider data. A connection's matched rule is available only while that connection is active.

Rules are preserved when subscriptions are refreshed or applied. If validation or a running-engine reload fails, Nagi keeps the previous rules file and reports the failure.
