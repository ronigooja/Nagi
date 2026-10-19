# Localization

**Status: implementation in progress.** This is the accepted localization design
and migration audit for English (`en`) and Simplified Chinese (`zh-Hans`). It is
not a claim that every interface is translated. The source inventory below was
captured from baseline `661a69c`; implementation and acceptance remain separate.

This document owns language selection, presentation boundaries, the additive
message metadata contract, terminology, and migration accounting. The
[CLI reference](../reference/cli.md) remains authoritative for command syntax,
JSON fields and exit codes; the [terminal menu design](terminal-menu.md) and
[UI rules](../development/ui-design.md) retain their interaction requirements.

## Implemented foundation

Phase A provides immutable Go language contexts, prefix-only language selection,
typed messages with preserved English errors and causes, additive JSON metadata,
and native App resource/package support. The three language-option messages are
translated; broad CLI, menu and native interface migration remains in progress.
The App preserves original business values and resolves optional descriptors at
presentation boundaries across normal, rewrite, capture and streaming transports.

Go catalog modules live under `internal/i18n/catalogs/en` and `zh-Hans`. Each
module declares its exact argument signatures in
`internal/i18n/catalogs/<module>.arguments.json`, mapping every message ID to an
argument-name/type object (`{}` for no arguments). Supported types are `string`,
`number` and `boolean`. Loading rejects duplicate keys/IDs, language/schema ID
drift, invalid plural forms, and inconsistent placeholders. Plural messages use
a numeric `count` and `one`/`other` forms; Chinese may define only `other`.
The Go and native App foundation tests consume the shared contract fixture to
check initial IDs, typed argument fallback and resource agreement.

The App uses its installed resource bundle before SwiftPM's development fallback;
packaging includes both localizations and the existing menu image. Localized
resources are selected at process launch; restart the App after changing its
macOS language preference. Live refresh of existing windows is not implemented.

## Ownership and engine boundary

Localization belongs to Nagi presentation. The pinned mihomo revision is
`a5df21a6a339a651bb50dca73b30f7679dbd0436` in
[`engine.lock`](../../engine.lock). No engine capability, configuration field,
API or pin change is required: Nagi continues to configure mihomo and invoke
its existing APIs. Localization changes no forwarding, matching, DNS, TUN or
rewrite protocol behavior.

The CLI owns command operations and typed results. Human CLI output, the
terminal menu, and the native App each render their own interface resources.
All three preserve operation identity, exit codes, persistence and activation.
English remains the fallback and the original JSON message language.

## Language selection

CLI language is resolved once per invocation, with this precedence:

1. Explicit `--lang en` or `--lang zh-Hans` before the command.
2. Nonempty `NAGI_LANG`.
3. Nonempty `LC_ALL`, then `LC_MESSAGES`, then `LANG`.
4. English.

The first nonempty environment setting decides the automatic language; an
unsupported setting falls back to English instead of checking a lower-priority
setting. Locale matching normalizes separators, encodings and modifiers.
English variants select `en`. Generic Chinese and Simplified Chinese locales
(`zh`, `zh_CN`, `zh_SG`, `zh-Hans`) select `zh-Hans`; Traditional Chinese
(`zh_TW`, `zh_HK`, `zh_MO`, `zh-Hant`) and unsupported languages select English.
Missing, repeated, or unsupported explicit language options are usage errors
with exit code `2`. The parser consumes `--lang` only in the global prefix;
command-following operands remain command-owned, including a literal `--lang`. Flags, command names and argument values retain their
spelling. `--json` still obeys its existing independent parsing rules.

The terminal menu inherits the invocation's resolved language, including child
streams. Concurrent reads use immutable language contexts rather than a global
mutable language setting. The CLI does not persist a language preference.

The App follows the macOS preferred language for its localized resource bundle.
The initial implementation provides no separate persisted App language setting;
macOS per-App language selection remains available through the operating system.
The App's selected resource language is independent of CLI environment variables
and never changes OS language settings. If language changes require an App
restart, document that behavior; do not promise live refresh of open windows.
App subprocesses continue to use English JSON values and optional metadata; the
App renders metadata in its own selected resource language.

## Stable messages across the JSON boundary

Existing JSON values, including English Nagi messages and diagnostic text, remain
unchanged. Producers may add an optional top-level `messages` object:

```json
{
  "ok": false,
  "error": {"code": "usage", "message": "Example English fallback."},
  "messages": {
    "/error/message": {
      "id": "cli.language.invalid",
      "args": {"value": "example-unsupported-locale"}
    }
  }
}
```

The example illustrates structure; its fallback is deliberately fictitious.
[`localization-contract.json`](localization-contract.json) records the initial
IDs, argument signatures and valid/invalid descriptor fixtures. Its version
tracks this fixture contract, not a new CLI envelope version.

- Each key is an RFC 6901 JSON Pointer into the existing envelope. `/data` is
  valid only when `data` itself is a string; nested string fields such as
  `/data/message`, `/data/checks/0/message` and `/error/message` may be annotated.
- A descriptor contains a nonempty stable `id` and optional `args` object.
  Argument values are JSON scalars: string, finite number or boolean. Null and
  container arguments are invalid. Empty arguments should be omitted.
- IDs describe semantic events, never English sentence text. A producer creates
  the descriptor where the event is known. Consumers must not infer IDs by
  matching, hashing, parsing or translating an English sentence.
- Each catalog defines exact argument names and types. Missing, extra, wrongly
  typed or nonfinite arguments invalidate that descriptor. Placeholder sets
  must match between languages. Dynamic names, paths, domains, URLs and external
  diagnostics are arguments, never translation keys.
- Consumers resolve only an existing string target and a known descriptor with
  a compatible signature. Invalid JSON Pointer syntax, invalid array indices,
  absent/nonstring targets, unknown IDs, malformed metadata and unsupported
  argument types preserve the original English value. A bad optional descriptor
  must not invalidate an otherwise valid command response or sibling descriptor.
- Metadata is presentation-only. Consumers branch on `error.code`, typed state,
  command identities and protocol fields. They never branch on localized text.
- Older consumers ignore the additive field; older producers without metadata
  remain readable using their original English messages. Human CLI localization
  never mutates the original result merely to feed a consumer.

The initial shared IDs are `cli.language.invalid` (`value: string`),
`cli.language.missing`, and `cli.language.duplicate`
(no arguments for the final two). New IDs and argument signatures must be
added to producer and consumer catalogs together. Originating errors retain
English `Error()` behavior and wrapping/unwrapping; presentation uses their
separate descriptor. Nagi-owned error wrappers can translate their context
while preserving a redacted external diagnostic argument.

Resource ownership is split to avoid concurrent catalog edits: CLI command and
help resources, human output resources, and terminal-menu resources are separate
Go modules/catalogs; App interface resources and the App's CLI-message catalog
are separate resources. A language context is passed explicitly through entry
points and renderers. English sentence lookup is not a resource API.

## Classification boundaries

These classifications apply to every occurrence, including text nested in
otherwise machine-readable objects.

| Class | Treatment | Source and symbol examples |
| --- | --- | --- |
| Translated | Nagi-owned labels, titles, summaries, descriptions, guidance, validation, empty states, failures, notifications, accessibility text and native dialogs use semantic catalog IDs. | `internal/command/help.go:commandSpecs`, `internal/output/output.go:Write`, `internal/menu/page.go`, `macos/NagiApp/Sources/NagiApp/NagiApp.swift:MenuAppDelegate` |
| Raw | Explicitly requested original content remains byte/content preserving. Only surrounding controls and sensitivity warnings translate. | `internal/output/output.go:writeRaw`, `internal/output/request_capture.go:writeCapture`, `internal/menu/runner.go:RunRaw`, App capture record/detail bodies and raw rewrite previews |
| Protocol | Keep JSON keys, error codes, commands, options, YAML fields, mode/status enum values, script APIs, RFC3339 timestamps, identifiers and built-in outbound names stable. Map to localized labels only at display boundaries. | `internal/command/completion.go`, `internal/menu/render.go:renderValue`, `AppModel.swift:ProxyGroup`, `RequestRewriteEditors.swift` picker tags |
| External diagnostics | Preserve sanitized OS, mihomo, HTTP, TLS, archive, script and decoder diagnostics; translate Nagi-owned context around them. Do not synthesize a translated diagnosis from diagnostic sentence matching. | `internal/control`, `internal/profile` validation diagnostics, `internal/platform`, `NagiCLIClient.swift:CLIError.launch`, `error.localizedDescription` at native OS boundaries |
| Out of scope | Development build/release tooling, test assertions/fixtures, vendored code, license text, documentation prose and user/provider-supplied resource names are not runtime interface catalogs. | `scripts/`, `macos/NagiApp/package-app.sh` build progress, `*_test.go`, `macos/NagiApp/Tests`, source import paths |

Raw content includes configuration and overrides, profile diffs, engine logs,
shell completion scripts and candidate output, rewrite JSON/YAML/script source,
public certificates and exports, HAR/native capture archives, original headers,
cookies, URLs, body bytes, WebSocket payloads and explicit preview results.
These workflows may contain sensitive information. Ordinary localized messages
and metadata arguments must use the same redaction as their English equivalents;
translation must not reintroduce a secret through a descriptor argument. No
argument carries a private key, subscription secret or original captured body.

Machine state such as `running`, `warning`, `request`, `websocket_client`,
`selector`, `rule`, `direct`, `fragment` and `forward_original` stays unchanged in
JSON and stored values. The renderer chooses a translated description. A node
actually named `Running`, `DIRECT`, or a Chinese word remains that exact name.
Time displays retain minute precision, while documented machine timestamp
formats remain unchanged. Numeric protocol units and byte values retain their
meaning; a label can explain their unit in either language.

## English terminology and Simplified Chinese glossary

| English | Simplified Chinese | Meaning |
| --- | --- | --- |
| Nagi / mihomo / Tailscale / Headscale | unchanged | Product names. |
| Proxy node / Proxy group / Proxy provider | 代理节点 / 代理组 / 代理提供方 | Keep provider distinct from subscription. |
| Subscription / Profile / Local overrides | 订阅 / 配置方案 / 本地覆盖设置 | A profile is a named Nagi configuration. |
| Engine / Selected profile | 引擎 / 当前配置方案 | A process being running is not a connectivity claim. |
| Running / Stopped / Starting / Unavailable / Unknown | 运行中 / 已停止 / 启动中 / 不可用 / 未知 | Display labels only. |
| Enable / Disable / Enabled / Disabled | 启用 / 停用 / 已启用 / 已停用 | Distinguish action from observed state. |
| System proxy / TUN / Listener ports / LAN access | 系统代理 / TUN / 监听端口 / 局域网访问 | Preserve TUN acronym. |
| Rule / Global / Direct mode | 规则 / 全局 / 直连模式 | CLI tokens remain `rule`, `global`, `direct`. |
| Routing rule / Rule set / Target | 路由规则 / 规则集 / 目标 | Do not translate rule payloads. |
| Latency / Automatic latency checks / Test URL | 延迟 / 自动延迟检测 / 测试 URL | Checks do not imply successful connectivity. |
| Download interval / Update and Apply | 下载间隔 / 更新并应用 | Download, save, apply, activate remain distinct steps. |
| Saved / Reloaded / Apply / Activate | 已保存 / 已重新加载 / 应用 / 激活 | Never imply rollback or activation without evidence. |
| Start at Login / Privileged helper / Recovery | 登录时启动 / 特权辅助程序 / 恢复 | The App owns its own login preference. |
| Request rewrite / Request capture | 请求改写 / 请求捕获 | Separate editing from original-content capture. |
| Certificate authority / Trust / Public certificate | 证书颁发机构 / 信任 / 公共证书 | Private key and public export remain distinct. |
| Before / After / Diff / Raw Result | 改写前 / 改写后 / 差异 / 原始结果 | UI identities must be stable enums. |
| Back / Cancel / Submit / Retry / Exit / Quit | 返回 / 取消 / 提交 / 重试 / 退出菜单 / 退出 | Context determines menu exit versus App quit. |
| Warning / Failed / Untested / No entries | 警告 / 失败 / 未测试 / 暂无条目 | Preserve unknown and failed distinctions. |

## Inventory and baseline coverage

[`localization-inventory.tsv`](localization-inventory.tsv) is a conservative
source audit checklist with file, baseline line, symbol anchor, interface area,
classification candidate, review status and literal fragment. It contains
4,964 display candidates from production Go and Swift files at the baseline.
Counts are estimates of occurrences, not unique semantic IDs or verified
user-visible messages. Concatenated strings and Swift interpolation can produce
multiple fragments; some apparent prose is protocol or an example. Multiline
literals, generated labels, dynamic backend diagnostics and imported content
require the boundary review above. The checklist must never be used by itself
to claim complete localization. No entry is marked implemented in this phase.

| Area | Candidate occurrences | Primary work |
| --- | ---: | --- |
| CLI command/help | 1,661 | Help summaries/descriptions/hints, parser/usage and operation errors. |
| Human CLI output | 235 | Labels, empty states, operation completion and fallback presentation. |
| Terminal menu | 793 | Navigation, prompts, forms, impacts, feedback, details and typed state labels. |
| Native App | 896 | AppKit and SwiftUI controls, windows, validation, menus, alerts, accessibility and transport errors. |
| Backend packages surfaced by CLI | 1,379 | Nagi-owned validation/operation diagnostics; review external/raw boundaries before migrating. |

The 1,379 backend candidates are required review work, not an exclusion. They
include `internal/profile`, `subscription`, `service`, `traffic`, `diagnostic`,
`installation`, `requestrewrite`, `capture`, `rewritedraft`, runtime and control
wrappers. Their external causes remain verbatim only after the existing
sanitization. Protocol fields inside their returned objects remain stable.

Key baseline hotspots are `help.go` (827 candidates), `request_rewrite_help.go`
(137), `output.go` (196), `NagiApp.swift` (157), `CaptureDraftWindow.swift` (140),
`RequestRewriteEditors.swift` (135), `command.go` (126), `profile.go` in menu
(93), `request_rewrite_rules.go` (86) and `request_capture.go` in menu (80).

The migration owner must replace each relevant candidate with an explicit ID,
classify each deliberate preservation by boundary, and inspect the containing
symbol for computed labels that literal scanning misses. Adding a lookup around
a concatenated English sentence does not satisfy this requirement. Full coverage
requires code review of rendering and error origins, not just catalog key counts.

## String-driven behavior and transport audit

These baseline paths require semantic refactoring before translating text:

| Path / symbol | Baseline behavior | Required change |
| --- | --- | --- |
| `internal/menu/render.go:renderValue`, `statusLabel` | Branches on English normalized field labels for timestamps, endpoint redaction, profile and state colors. | Retain original field identity and semantic style before choosing translated display labels. |
| `internal/output/style.go:StyleStateValue` | Classifies English state tokens for color. | Classify stable state token separately from translated text; never classify a resource name. |
| `AppModel.swift:refresh` | Clears errors by `hasPrefix("Status unavailable:")`. | Track typed error origin. |
| `AppModel.swift` traffic task | Clears errors by `hasPrefix("Traffic unavailable:")`. | Track typed error origin. |
| `SourceSettingsPanel.swift` validation reset | Detects `hasPrefix("Enter ")` in field text. | Track validation state independently of localized message. |
| `CaptureDraftWindow.swift:CaptureDraftPreview` | Uses `Before / After`, `Diff`, `Raw Result` as picker identities and branch values. | Stable tab enum/tags with translated labels. |
| Rewrite/capture picker tags | Protocol kind/stage/component values also appear as generated labels. | Preserve tags; map display labels by semantic value. |
| Menu `Item.Selected` and feedback kind | Already carries semantic selection and severity. | Preserve typed state; do not regress to matching labels. |

Every CLI JSON decoding path must preserve optional metadata and localize the
presentation without changing typed operation data:

- Menu `commandRunner.RunInput` decodes both success and failure envelopes;
  `Read` runs it concurrently. `Stream` and `RunRaw` preserve their distinct
  streaming/raw contracts and language context.
- App `NagiCLIClient.runBlocking` covers ordinary reads and mutations;
  `rewrite`, `capture`, and `rewriteTransport` each decode independently.
- App `SubscriptionWatch.parse` decodes NDJSON success/error lines. The traffic
  reader decodes samples and separately decodes error stderr in `wait`.
- Typed `CLIEnvelope<Value>` and JSON-value payloads must both tolerate absent,
  unknown and malformed optional metadata. Successful capture results contain
  intentionally sensitive raw values and must not be recursively translated.
- Local launch, cancellation, timeout, size, missing-field and invalid-response
  errors have native IDs; preserved OS/decoder detail is a separate argument.

## Migration ownership and acceptance

Phase A establishes contracts/resources and this audit; it does not complete
feature catalogs. Parallel Phase B ownership should be exact and disjoint:

| Owner | Files and resources | Required review |
| --- | --- | --- |
| CLI | `internal/command/**`, `internal/output/**`, `cmd/**`, CLI catalogs; then dedicated backend diagnostic pass. | Every command spec, output branch, Nagi-owned error origin, nested message pointer and completion/raw exception. |
| Menu | `internal/menu/**`, menu catalog. | Every page/form/selector, styles, retained forms, command metadata, read/stream/raw paths and effects. |
| App | `macos/NagiApp/Sources/**`, tests, Package.swift and packaging resource declarations. | All AppKit/SwiftUI surfaces and all transports; typed state/tab fixes and open-window language refresh. |
| Integration/docs | README, architecture summary, CLI reference, App README, build/install documents and final status here. | Final behavior matches documented options, fallback and package layout; links and examples are valid. |

Backend work can be delegated after one UI owner finishes, but must have explicit
package ownership and retain event descriptors at error origins. Do not classify
all `err.Error()` strings as external: many originate inside Nagi.

Acceptance requires catalog parity, exact typed placeholder checks, immutable
context/concurrent-read tests, CLI precedence and syntax failures, English JSON
compatibility, valid/invalid metadata fallback, and representative command
success/failure/empty output in both languages. Raw configuration, log, export,
completion, preview and capture samples must retain their original content.
Menu checks include real numeric navigation, cancellations, retained forms,
feedback, styling/redaction and stream return in both languages. App checks
include every envelope path and typed error clearing under Chinese presentation.

Native verification uses `swift test --package-path macos/NagiApp` and
`make build app`, then inspects both language resource directories and the menu
PDF inside the installed resource bundle. Verify macOS preferred and per-App languages,
all window families, long translations, minimum size, light/dark appearances,
focused/disabled controls and documented restart behavior for already-open windows. Packaging must
retain the menu image and add `en`/`zh-Hans` localizations without embedding
source paths or breaking bundle resolution.

The Linux audit host has Go under `$HOME/environment/go1.26.4`; no native Swift
or macOS SDK was found in PATH. No dependency was downloaded. Earlier native
build and test evidence is recorded in the
[verification matrix](../development/request-rewrite-acceptance.md), but does
not establish localization acceptance. No remote host address or credential is
part of this design. Cross-compilation cannot establish native window or
resource behavior; record any remaining native checks explicitly.
