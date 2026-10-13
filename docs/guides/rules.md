# Manage routing rules

Use the CLI to inspect mihomo's active order and keep local rules separate from subscription profiles.

```sh
nagi rules list
nagi rules providers
nagi rules add local-direct DOMAIN-SUFFIX intranet.example DIRECT
nagi rules import ads domain ./ads.yaml REJECT
nagi rules conflicts
nagi rules test 154.19.187.117
nagi rules connection CONNECTION_ID
```

Custom rules are evaluated before profile and subscription rules. Disable a named rule or import temporarily with `nagi rules disable NAME`; re-enable it with `nagi rules enable NAME`. Remove the entire named rule, full-rule import, or set with `nagi rules remove NAME`.

To import full mihomo rules with different targets, save a file such as `personal-rules.yaml`:

```yaml
rules:
  - DOMAIN-SUFFIX,intranet.example,DIRECT
  - DOMAIN-SUFFIX,ads.example,REJECT
  - IP-CIDR,192.0.2.0/24,DIRECT,no-resolve
```

Then run `nagi rules import personal rules ./personal-rules.yaml`. The import preserves every rule's target, options, and order as one named object. Do not supply a common target for the `rules` format. A YAML string list is also accepted. If the file contains other profile fields, only its `rules:` list is imported. Referenced `RULE-SET` providers and `SUB-RULE` definitions must already exist in the effective profile.

For a domain set such as `ads.yaml`, use a provider payload:

```yaml
payload:
  - '+.ads.example'
```

Run `nagi rules import ads domain ./ads.yaml REJECT` to route the whole set to one target. The `classical`, `domain`, and `ipcidr` formats require that target. Use the same command with an HTTP(S) URL, for example `nagi rules import local-net ipcidr https://example.com/networks.txt DIRECT`. Local files and HTTP responses are limited to 8 MiB; remote imports are saved snapshots and are not automatically refreshed.

In `nagi menu`, choose **Settings & Maintenance → Routing Rules → Import rule file**. Select the format numerically, then enter a name and local path or HTTP(S) URL. Full rules do not ask for a common target. Failed imports retain entered values for editing and explicit retry. **Manage Rules and Sets** enables, disables, or removes whole named objects; removal deletes all rules in a full-rule import. The former `import-local` and `import-remote` commands have been removed. See the [import reference](../reference/cli.md#rule-file-imports) for exact input formats.

Use `nagi rules conflicts` to find duplicate matchers and rules placed after `MATCH`. The report cannot infer every semantic conflict in regular expressions or provider data. A connection's matched rule is available only while that connection is active.

Rules are preserved when subscriptions are refreshed or applied. If validation or a running-engine reload fails, Nagi keeps the previous rules file and reports the failure.

To test a domain or IP interactively, open `nagi menu` and choose **Settings & Maintenance → Routing Rules → Test Rule**. Start the matching mihomo engine first. Enter a bare domain or IP address; the menu tests TCP port 443 against the engine's active rules, including imported sets, and shows the rule and outbound target.

The test does not connect to the destination. Mihomo can perform DNS lookups when a rule needs a domain's address. To specify an address or different port, use `nagi rules test service.example --ip 192.0.2.1 --port 80`. Tests evaluate rules even in global/direct mode and report that the mode bypasses them for ordinary traffic. Source, inbound, process-path, UID, and sniffing context can change real matches; inspect an active connection to confirm its actual rule. See the [destination test reference](../reference/cli.md#destination-rule-tests) for complete options, output, and limitations.
