# Manage routing rules

Use the CLI to inspect mihomo's active order and keep local rules separate from subscription profiles.

```sh
nagi rules list
nagi rules providers
nagi rules add local-direct DOMAIN-SUFFIX intranet.example DIRECT
nagi rules import personal ./personal-rules.yaml
nagi rules conflicts
nagi rules test 154.19.187.117
nagi rules connection CONNECTION_ID
```

Stored rules belong to the selected profile. Select the intended profile with `nagi profile use NAME` before adding, importing, listing with `rules custom`, or managing them. Custom rules are evaluated before that profile's source and subscription rules. Switching profiles activates the new profile's own rules; an imported rule targeting a subscription group does not affect `default`. Disable a named rule or import temporarily with `nagi rules disable NAME`; re-enable it with `nagi rules enable NAME`. Remove the entire named rule, full-rule import, or set with `nagi rules remove NAME`.

To import full mihomo rules with different targets, save a file such as `personal-rules.yaml`:

```yaml
rules:
  - DOMAIN-SUFFIX,intranet.example,DIRECT
  - DOMAIN-SUFFIX,ads.example,REJECT
  - IP-CIDR,192.0.2.0/24,DIRECT,no-resolve
```

Then run `nagi rules import personal ./personal-rules.yaml`. The import preserves every rule's target, options, and order as one named object. Each rule supplies its own target; the import command has no format or common-target argument. A YAML string list is also accepted. If the file contains other profile fields, only its `rules:` list is imported. Referenced `RULE-SET` providers and `SUB-RULE` definitions must already exist in the effective profile.

Use the same command with an HTTP(S) URL, for example `nagi rules import remote https://example.com/rules.yaml`. Local files and HTTP responses are limited to 8 MiB; remote imports are saved snapshots and are not automatically refreshed. Imports accept complete native rules only; provider `payload:` files and domain/IP-CIDR text lists are not supported.

In `nagi menu`, choose **Routing & DNS → Routing Rules → Import rules**, then enter a name and local path or HTTP(S) URL. Failed imports retain entered values for editing and explicit retry. If validation identifies a missing proxy target, replace that target with an existing node, proxy group, or built-in such as `DIRECT`, or add the intended target to the selected profile before retrying. Missing-outbound import errors show the target and a one-based rule number in the generated candidate configuration. JSON error messages retain the profile and native diagnostic path; `rules[0]` means its first rule. After a validation failure and successful restoration, Nagi explicitly reports that imported rules were not saved and the runtime configuration was not changed. **Manage Rules and Sets** enables, disables, or removes whole named objects; removal deletes all rules in a full-rule import. Previously imported provider sets remain manageable, but new provider-set imports are not supported. The former `import-local` and `import-remote` commands have been removed. See the [import reference](../reference/cli.md#rule-file-imports) for the exact input contract.

Use `nagi rules conflicts` to find duplicate matchers and rules placed after `MATCH`. The report cannot infer every semantic conflict in regular expressions or provider data. A connection's matched rule is available only while that connection is active.

A profile's rules are preserved when its subscription is refreshed or applied. Removing a profile also removes its stored rules. The former global `config/rules.yaml` is not loaded or migrated; select each desired profile and import its rules explicitly. If validation or a running-engine reload fails, Nagi keeps the previous rules file and reports the failure.

To test a domain or IP interactively, open `nagi menu` and choose **Routing & DNS → Routing Rules → Test Rule**. Start the matching mihomo engine first. Enter a bare domain or IP address; the menu tests TCP port 443 against the engine's active rules, including imported sets, and shows the rule and outbound target.

The test does not connect to the destination. Mihomo can perform DNS lookups when a rule needs a domain's address. To specify an address or different port, use `nagi rules test service.example --ip 192.0.2.1 --port 80`. Tests evaluate rules even in global/direct mode and report that the mode bypasses them for ordinary traffic. Source, inbound, process-path, UID, and sniffing context can change real matches; inspect an active connection to confirm its actual rule. See the [destination test reference](../reference/cli.md#destination-rule-tests) for complete options, output, and limitations.
