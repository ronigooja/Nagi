# Manage routing rules

Use the CLI to inspect mihomo's active order and keep local rules separate from subscription profiles.

```sh
nagi rules list
nagi rules providers
nagi rules add local-direct DOMAIN-SUFFIX intranet.example DIRECT
nagi rules import-local ads domain ./ads.yaml REJECT
nagi rules conflicts
nagi rules test 154.19.187.117
nagi rules connection CONNECTION_ID
```

Custom rules are evaluated before profile and subscription rules. Disable a rule temporarily with `nagi rules disable NAME`; remove it with `nagi rules remove NAME`. Imported remote sets are downloaded and stored as snapshots with `nagi rules import-remote NAME BEHAVIOR URL TARGET`.

Use `nagi rules conflicts` to find duplicate matchers and rules placed after `MATCH`. The report cannot infer every semantic conflict in regular expressions or provider data. A connection's matched rule is available only while that connection is active.

Rules are preserved when subscriptions are refreshed or applied. If validation or a running-engine reload fails, Nagi keeps the previous rules file and reports the failure.

To test a domain or IP interactively, open `nagi menu` and choose **Settings & Maintenance → Routing Rules → Test Rule**. Start the matching mihomo engine first. Enter a bare domain or IP address; the menu tests TCP port 443 against the engine's active rules, including imported sets, and shows the rule and outbound target.

The test does not connect to the destination. Mihomo can perform DNS lookups when a rule needs a domain's address. To specify an address or different port, use `nagi rules test service.example --ip 192.0.2.1 --port 80`. Tests evaluate rules even in global/direct mode and report that the mode bypasses them for ordinary traffic. Source, inbound, process-path, UID, and sniffing context can change real matches; inspect an active connection to confirm its actual rule. See the [destination test reference](../reference/cli.md#destination-rule-tests) for complete options, output, and limitations.
