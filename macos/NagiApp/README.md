# Nagi macOS menu app

The macOS 13+ app lives only in the menu bar. It has no Dock icon. **Connections…** opens an independent, resizable window; reopening it brings the existing window to the front. The status menu separates engine status and recovery; daily controls (**System Proxy**, **TUN**, **Mode**, **Proxy Groups**); configuration management (**Profiles**, **Subscriptions**, **Proxy Providers**); traffic tools (**Connections…**, **Request Capture…**, **Request Rewrite**); App settings (**Display**, **Login**); and **Help**, **About Nagi**, and **Quit Nagi** with separators. Under **Proxy Groups**, selectable groups show their current selection beside the group name when available, without starting a latency test. Other group types retain their names without implying a single selected node.

Selecting a profile name directly under **Profiles** switches to that profile; the current profile is checked, including `default`. **Profiles → Manage Profiles** contains a **Delete “NAME”…** action for each personal profile. The current profile's deletion item is disabled and says to switch profiles first. The `default` template cannot be deleted. Each deletion action opens one confirmation naming the profile and explaining its cleanup scope; **Cancel** has the default Return key and initial focus. Canceling dispatches no CLI operation. If the current profile is unknown, deletion remains disabled. It calls `profile remove NAME` and refreshes profiles and subscriptions after success. Configuration, backups, local overrides and settings, and saved proxy choices are deleted. Subscription associations are cleared; subscription records and caches are kept. The CLI owns cleanup, association removal, selected-profile protection, and transaction recovery; see [profile removal](../../docs/reference/cli.md#profile-removal). **Apply Default Runtime Settings to Current Profile** appears in the shared Profiles menu after a separator below the profile list. It toggles safe default runtime settings for the current profile through the CLI. The item is checked when `profile override default-status NAME` reports the applied settings active. Selecting a checked item calls `profile override restore-default NAME` to restore the previous local override; proxy nodes, groups, rules, and Nagi-managed fields are excluded from the applied settings. It starts Nagi on launch, accepts an already running engine, and keeps the menu available if startup fails so Retry Start can be used. Quit runs the CLI cleanup command before the app terminates; a failed cleanup leaves the menu open with the CLI error.

Install the matching Nagi CLI as an executable at `/usr/local/bin/nagi`. Install the pinned mihomo executable beside the CLI, or provide `NAGI_MIHOMO_BIN` in the app's environment. For a published release, run `nagi app install` after [installing the CLI](../../docs/development/install.md#online-install-and-update); for a local build, [install from an artifact directory](../../docs/development/install.md), optionally including `Nagi.app` (installed to `/Applications/Nagi.app` by default). The App is unsigned and unnotarized, so macOS may require **Open** from the Finder context menu on first launch. The app invokes structured `nagi --json` commands and explicit raw request-rewrite exports; it does not read Nagi configuration files or contact mihomo's socket or API. The [CLI reference](../../docs/reference/cli.md) defines commands and JSON fields.

Choose **About Nagi → Update** to update the CLI, mihomo, and installed App together. Acknowledge the short informational prompt; no download or installation progress is shown. The App calls `nagi update --app`. The CLI downloads and verifies the release and obtains native macOS administrator authorization when needed before the App completes its normal quit cleanup. A detached updater installs the prepared package and reopens the App. macOS handles credentials; Nagi does not collect passwords. Preparation, authorization, launcher, and quit failures leave the App available with the error at the top of its menu. The updater cancels if the App has not quit within 60 seconds. After an installation failure, it persists a sanitized error through the CLI and attempts to reopen the App, which shows that error at the top of its menu. Routine state refreshes retain the update error; starting another explicit Update attempt clears it. On launch, the App reads and consumes the persisted failure through `nagi app update-error`. If reopening fails, recovery details remain in the per-user update log. See [installed CLI maintenance](../../docs/development/install.md#installed-cli-maintenance) for service restoration and recovery. Use `nagi app uninstall` to remove only the App after disabling its login item and quitting, or `nagi uninstall` to remove the whole installation while preserving user data.

The `default` profile entry uses Nagi's built-in configuration template, which installation and updates replace with the installed version's defaults. Use another name for personal profiles and subscriptions; see the [installation workflow](../../docs/development/install.md#install-update-and-remove).

The app owns a dedicated `subscription watch` child process and cancels it on sleep and quit; scheduling and apply behavior remain in the CLI. The default macOS installer configures the privileged CLI backend for TUN; see [traffic access](../../docs/guides/traffic-access.md). The app uses the selected backend through the same CLI commands, so changing backends does not require rebuilding the app.

If another scheduler host already runs, the app leaves that host alone and periodically retries acquiring its own host. An unexpected exit is shown as a scheduler error and retried with bounded delay. Sleep stops only the app-owned host; wake starts it again. Failed CLI quit cleanup leaves the app and its host running. Proxy group icons come from the CLI's `icon` field: `system:` names use SF Symbols, and HTTP(S) image URLs load asynchronously with time and size limits. Invalid or unavailable icons leave the group label without an image.

The status item uses Nagi's continuous-line N artwork as a 22 × 18 point template image. AppKit adapts the symbol and traffic text to the menu-bar appearance. Starting, stopped, and unavailable draw the whole symbol at half opacity; running draws it at full opacity. While applying a setting, the whole symbol alternates between half and full opacity every 0.4 seconds; traffic text stays at full opacity. The item also displays mihomo's upload and download rates once a second from `nagi --json traffic watch`. Traffic rates is the default Display choice, with the icon vertically centered beside two right-aligned rate rows at fixed width and no arrows; Icon only is the other choice. The numeric display uses the 9-point system font at Regular weight with natural character spacing. The text area is fixed at 45 points, directly beside the icon with no gap (67 points including the icon). A missing or stale sample, or a stopped engine, displays `0 B/s` for each visible rate. A saved Download only choice falls back to Traffic rates. The app reconnects the stream after interruption while the engine is running. Display choice is the only app data stored in UserDefaults; connection search and sorting last only for the window session.

While a menu command is applying a setting, the symbol animates. During a busy operation, the app closes any open status menu and temporarily detaches it so it cannot be opened. The menu becomes available again when the operation and its required reads finish or report an error. The app rebuilds the menu from the displayed state before opening it.

The app uses command results to update mode, proxy selection, TUN, system proxy, and login service state directly. Mode menu changes are temporary; **Save Current Mode as Default** invokes the persistent `mode save` command. TUN enable/disable also records the startup preference, which the CLI reapplies after lifecycle startup. Other operations refresh only the affected controls: automatic selection refreshes groups; provider operations refresh groups and providers; subscription update-interval and test-URL changes refresh subscription metadata; Provider update-interval and test-URL changes refresh groups and providers; profile and subscription activation refresh the affected profile and runtime state. Periodic reads reuse metadata briefly. Opening the menu or waking the Mac requests fresh state, and requests made during an operation are queued for reconciliation. The animation stops when the command and its required reads finish or report an error. See the [refresh and command completion design](../../docs/design/runtime.md#macos-app-refresh-and-command-completion) for coordination, cache intervals, and failure handling.

**Proxy Providers → Add Provider…** opens a form for the name, subscription URL, existing or new group name, update interval, and optional latency test URL. The update interval accepts `off`, 1 through 31,536,000 seconds, and common choices of 1, 6, or 24 hours. It remains available when stopped or when the live provider list cannot be read. The form calls `proxy provider add` and retains input after a validation or CLI failure for explicit retry; error messages redact URLs. Success refreshes running groups and providers. Creation saves to the selected profile's local override and does not start a stopped engine. New groups leave routing rules unchanged; choose their nodes under **Proxy Groups** and route traffic using existing rules or global mode. Explicit and automatic subscription application retain local overrides, including added providers, and rebase local node/group changes against the new feed. Each listed provider shows its type and node count, then offers **Update Provider**, **Test Latency → All Nodes** or an individual node, **Update Settings…**, **Latency Check Settings…**, and **Delete Provider**. After a successful **Update Provider**, the status summary shows **Nagi · Info** with `Provider “NAME” updated.` in its submenu. Failed updates show an error without a success notice. Delete removes the definition and group references through `proxy provider remove`, then refreshes groups and providers. The [provider guide](../../docs/guides/proxy-providers.md) covers the workflow and links to the authoritative CLI contract.

**Profiles → Apply Default Runtime Settings to Current Profile** applies runtime settings through the current profile's local override while retaining its nodes, providers, groups, and rules. Selecting the checked action restores the previous override. See the [default runtime settings contract](../../docs/reference/cli.md#local-profile-files-and-overrides).

## Language and resources

The App supports English and Simplified Chinese through macOS preferred and
per-App language selection. Restart the App after changing that preference;
already-open windows do not refresh their language in place. CLI environment
language settings do not override the App's selected resource language.

AppKit and SwiftUI interface strings, plural counters, tooltips, accessibility
labels and known protocol display states use `NagiResources.bundle`. Names,
configuration, scripts, previews and captured original traffic retain their
original content. CLI response values remain unchanged; optional typed message
descriptors localize supported diagnostics at presentation boundaries. Missing,
unknown or malformed metadata preserves the original response text.

After adding CLI producer messages, maintainers regenerate the checked-in native
signatures and bilingual descriptor resources from the repository root:

```sh
python3 macos/NagiApp/Tools/update-message-catalog.py
```

Packaging includes `en.lproj`, `zh-Hans.lproj`, `MessageSignatures.json` and the
existing menu PDF. The installed resource bundle is preferred over the SwiftPM
development fallback. Native automated bilingual tests and relocated/package
resource checks have passed at the migration checkpoint. Manual interaction,
focus/scrolling and complete light/dark/minimum-size acceptance remain pending;
the earlier five layout assertion failures were resolved with native minimum-size
constraints and mounted accessibility fixtures. Automated fixtures do not replace
interactive desktop acceptance. See the
[localization design](../../docs/design/localization.md) for coverage and gates.

## Node latency and automatic checks

Open **Proxy Groups → GROUP** to see ordinary nodes' latest engine latency, **Failed**, **Untested**, or **Unknown**. Successful measurements below one millisecond display **<1 ms**. Node tooltips show the measurement time to the minute and sanitized test origin. Nested groups show their name, selection mark, and the latest measurement of their actual selected ordinary leaf, without the leaf's name. If that leaf is a built-in action, the group shows **DIRECT** or **REJECT** instead of a measurement. A group shows **Unknown** when no unique leaf can be resolved. LoadBalance has no inferred unique leaf state. DIRECT and REJECT entries keep only their names and existing selection marks. Opening menus reads existing state without starting a test; selecting a node immediately refreshes actual group state.

**Test Single Node → NODE** measures an ordinary node in its exact group or Provider context using that context's URL, timeout, and expected HTTP status. Built-ins and nested groups have no individual node-test entry. **Test Current Group** uses the group's configured conditions. The App disables this action when every member is a built-in REJECT/REJECT-DROP action or a nested group currently selecting one; the CLI still calls mihomo and reports an expected reject-only result without an error. For URLTest/Fallback the tooltip explains that testing restores automatic selection: mihomo clears fixed selection before measuring and Nagi removes its saved choice when confirmed. This can occur even when a measurement fails. Tests refresh observations after success or failure. The pinned engine shares identical in-flight node tests and reuses successful results for five seconds, including manual and automatic checks; reuse retains the original measurement time. **Proxy Providers → Test Latency → All Nodes** invokes mihomo’s native Provider healthcheck; the node entries use their Provider context.

Provider and Subscription menus each have separate **Update Settings…** and **Latency Check Settings…** entries with matching native panels. Each panel is 520 points wide, scrolls when its content exceeds the available screen height, and stays open during reads and saves while blocking interaction with other App windows. Update Settings loads the saved values into an automatic-update checkbox and interval choices of 1, 6, or 24 hours, plus custom seconds from 1 through 31,536,000. Save becomes available when you change a valid setting. Turning off automatic updates disables the interval input, retains its draft, and saves `off`. For a Provider this controls node-source downloads; for a Subscription it controls scheduled download and apply. Providers with an original source interval also offer **Restore Original Interval** and preview its value. Latency Check Settings has no enable switch: its interval and URL sections independently choose **Use inherited settings** or a custom value. Check interval choices are 5, 10, or 30 minutes, 1 hour, or custom seconds in the same positive range. The saved check interval and inheritance choices are selected when the panel opens. The URL field shows the complete saved test URL, including its path and query, and can be edited after selecting **Custom URL**. Inherited intervals and complete URLs appear in disabled fields. Differing values show **Varies by group or provider**; unapplied subscriptions show **Not yet applied**. Switching to inherited replaces the current input; switching back to custom edits the inherited value. Saving retains the entered URL; unchanged values do not enable Save. A Provider without a usable inherited test URL cannot restore it; the panel explains why. Each latency setting has its own Save action and CLI operation; successful saves remain applied if another setting fails, while unsaved edits stay in the panel. Invalid or unchanged drafts cannot be saved. Reads and saves disable editing and closing until they finish. Closing with unsaved changes asks whether to discard them. Validation, load, and save failures appear inside the panel and retain editable drafts. If saving succeeds but reading current values fails, the section reports that it was saved and offers **Refresh Saved Values**, which only reads and never repeats the save. A subscription latency policy can be saved before first apply and follows its associated profile through updates. Provider local latency intervals take precedence over the subscription policy, then the original configuration. Saving an interval does not enable disabled checks; lazy checks may skip unused nodes. Group menus have no interval or URL editor; advanced per-group settings remain in the [CLI interval interface](../../docs/reference/cli.md#automatic-latency-check-intervals).

## Request rewrite

Choose **Request Rewrite → Request Rewrite…** to open a separate resizable native window; reopening brings the existing window forward and preserves its size. The initial frame fits within 90% of the screen's visible area, up to 1080 × 760 points. Tab contents do not determine the window size; long messages and certificate details scroll within the available space. Rule and script editors fit within 85% of the visible screen dimensions. These layout changes require macOS visual and interactive acceptance. **Rules** is the initial tab. The window title includes the current profile, which owns the saved rules and scripts; it is not a directory path. The content has no duplicate title or profile banner. The toolbar shows actual state separately from the enable/disable action, and its help button opens setup and coverage details. Enable requires fresh CLI status confirming running TUN, or fresh rule lists confirming an empty enabled rule set for idle intent. The CLI also checks engine capability, CA and trust; an old CLI or incompatible engine reports an actionable error. This feature requires the matching compatible engine; see the [CLI contract](../../docs/reference/cli.md#tun-request-rewrite).

**Status** shows the saved enable setting, actual state, TUN and rewrite service evidence, and CA/trust as labeled values, with reported errors below. **Runtime Details** expands schema, configured and active versions, generation, engine epoch, backend and protocol. Missing runtime evidence displays `unknown`. Validate, import and export remain available for recovery, with disable in the toolbar. Import replaces the complete disabled document while retaining existing intent. Export may contain credentials and full URLs; keep it private. Existing export destinations are rejected by the CLI.

**Rules** creates, edits, enables, disables, removes and reorders v2 rules. Enabled rules select interception domains directly; there is no separate domain list. An empty enabled rule set reports idle while retaining enabled intent. The editor covers request, response and both WebSocket directions, all original-metadata predicates, fragment/full range modes, ordered header/URL/method/status/JSON/body/text/regex/trailer/script operations, error policy and destination consent. Common match fields stay visible; **Additional Conditions**, **Advanced Settings** and **Allowed Destinations** expand other fields. Existing values expand their sections when the editor opens; choosing a URL scheme, host or port operation also expands allowed destinations. Destination consent authorizes a replacement target without adding interception coverage. **Scripts** adds, edits and removes saved scripts with metadata-only or buffered body access. Scripts run only through rule operations. **Edit Rule…** and **Edit Script…** load complete values through raw CLI export and display them directly, without masking or a reveal step. Lists show rule metadata and operation names or script metadata; editors show full operation values, predicates, destination consent and source. Large JSON numbers retain their exact digits. Refresh failures disable actions that depend on stale lists. Drafts and error messages remain visible after failed saves; the editor rereads state so you can correct the draft and explicitly retry without a separate refresh button. Cancel before saving persists nothing, and mutations are never automatically retried. Field help and help buttons provide format and script details on demand.

**Local Preview** accepts a v2 envelope through native request/response/exchange/WebSocket forms or a private fixture file. Forms support headers, trailers and UTF-8 or base64 binary bodies. A draggable horizontal divider separates the scrollable input and result panes, with roughly three quarters of the initial space assigned to input. Request, response and WebSocket body editors are 180 points tall and scroll internally. The action buttons remain outside the scrollable panes. **Run Preview** and **Preview File…** show original result values directly without DNS, network requests or persistence. **Clear Result** remains available when a result exists, independently of preview execution. **Certificate & Trust** presents certificate metadata as labeled values and separates local CA generation, public-only certificate export, login keychain trust installation and removal. macOS may request authorization. Disable retains the CA and trust; trust removal tears down rewriting while retaining enabled intent and the key. Independent application trust stores need separate configuration.

The window does not poll. Opening or restoring it, returning to the foreground, successfully completing an operation, or choosing the toolbar’s **Refresh** reads current state and lists, without overlapping reads. Opening the menu bar menu also reads rewrite status; the App’s periodic general refresh does not. Rule and script editors have no refresh button. Close, minimize, hide, sleep, settings and quit cancel and drain reads, including a sensitive edit export. Returning to a visible window reads once. Window close preserves the service and in-memory drafts; a submitted operation can finish after close. Settings and quit serialize with submitted mutations and file exports. App Quit uses CLI cleanup; failed cleanup leaves the app available and restores eligible reading. The app passes sensitive documents through bounded stdin pipes and uses explicit raw CLI exports without `--json`; it never reads configuration or contacts the engine directly. Human times use local minute precision. Rewrite document stdin is capped at 1 MiB; raw/JSON document output permits up to 6 MiB of escaped content plus an envelope. Preview alone permits 24 MiB input and 72 MiB output plus an envelope. Other CLI output limits are unchanged.

The implementation includes Swift fixtures for models, window reuse, profile title updates, lifecycle coordination and real CLI child-process boundaries. The updated layout was authored and source-reviewed on Linux without Swift or macOS frameworks; compilation, tests and interactive acceptance of these UI changes remain **unverified**. Earlier native evidence is recorded in the [verification matrix](../../docs/development/request-rewrite-acceptance.md). On macOS, run `swift test --package-path macos/NagiApp` and `make build app`, then verify profile title updates, collapsed sections, form sizing, full-value editing and preview, preview divider dragging and body scrolling at minimum window size in light and dark appearances, retained drafts, keyboard editing, file pickers, visibility/minimization, menu toggles and quit failure recovery.

## Active connections

Choose **Connections…** in the status menu, including while the engine is stopped. The native table shows destination, process, protocol, proxy chain, and cumulative uploaded/downloaded bytes. Click a column to sort; the initial order is destination, with connection ID breaking ties. Search domains, IPs, process names or paths, proxy chains, and IDs. The count shows matches / total. Select a row to see its source and destination, process path, inbound type, matched rule and payload, chain, and ID from the same snapshot. Missing fields display `unknown`. Selection and scrolling follow IDs as the list changes; a disappeared selection clears the details with a message.

The window reads immediately on opening and attempts a new snapshot every second. A slow request skips timer ticks; **Refresh** uses the same request control. A connection lasting less than a second may be missed. Closing, minimizing, hiding the window or app, system sleep, and quitting cancel reads. Returning to a visible window or waking resumes with an immediate read. If macOS declines native minimization for the accessory App, the window hides instead; **Connections…** restores it. An engine stop clears the list; use **Retry Start** in the status menu to resume. A read failure preserves the last snapshot, marks it stale, and disables close actions until another read succeeds. A list exceeding a response limit pauses automatic refresh and offers manual retry without truncating data.

**Close Connection** submits the selected ID once. **Close All Connections…** asks for confirmation and affects all current mihomo connections, including those excluded by search. Applications may reconnect automatically. Success means mihomo accepted the request, including an ID that already disappeared; no closed count is reported. Failures and timeouts appear in the window and are never automatically retried. Close operations are coordinated with settings and quit cleanup. Connection reads do not animate the status icon or rebuild the menu. See the [CLI connection contract](../../docs/reference/cli.md#proxy-latency-connections-and-mode) and [window design](../../docs/design/connections-window.md).

## Capture window

**Request Capture…** opens the native request browser. Start a session explicitly after
configuring TUN and certificate trust through the existing controls. Starting
capture never enables TUN or installs trust. Capture applies to new connections;
create a new connection in the application after starting it.

The toolbar's **Scope: All traffic** control opens a popover for the next
capture. Edits stay in the window; **Start** uses them directly. During recording,
pause or uncertain recovery, the control shows the actual session scope for
inspection; stop capture before editing it. The **More** (ellipsis) menu contains
the next-session **Body limit**, **Import…**, **Export** and **Clear** actions.

While recording, the window refreshes status and records every second. Paused
or stopped sessions do not poll; use **Refresh** to synchronize external changes.
Opening or restoring the window or returning to the foreground synchronizes once.
If a prerequisite is unmet, use **Recheck** beside its message after resolving it;
**Refresh** only updates capture data. See the [capture design](../../docs/design/request-capture.md#interaction-and-sensitive-presentation)
for lifecycle and failure behavior.

The scope accepts domain patterns, exact process paths and application bundle
paths, including paths entered before the application appears in **Find
Applications**. Empty scope captures all applications. Different scope categories
combine; entries within one category are alternatives. The selected scope and
body limit apply to the next session. Use **Pause**, **Resume**, **Stop** and
**Clear** to manage the current session. The menu bar also shows the recording
state and offers **Stop Capture**.

The request table separates **Method**, **Request**, **Status** and **Time**.
The Request column shows the host above its path/query; hover for the complete
URL or use **Copy URL**. HTTP Status shows the actual response code, falling
back to the original response, or Pending/Failed when no code was captured.
WebSocket rows show message direction and sequence without borrowing the
handshake's status. Process, start time, matched rules and record IDs appear in
the detail overview. Search uses original URLs, headers and bodies; kind, domain
and process filters narrow the table. Refresh retains selection by record ID.

Select a record and snapshot, then choose **Overview**, **Headers** or **Body**.
Overview shows complete metadata, collection state and related WebSocket
navigation. Headers has its own scroll area for complete header/trailer values.
Body gives the remaining height to raw text, formatted JSON, hex or before/after
comparison, with format and copy controls above it. **JSON Pointer…** on Body
opens a field-rule popover. Header, URL, method, status and selected body text
context menus open a quick-rule draft from that field. The draft supports the full
existing operation set, match predicates, insertion position and enable-on-save.
The quick-rule window title identifies the destination profile. Rule order uses
**Before RULE** or **At end**; additional match conditions and advanced settings
expand on demand, while **Preview…** opens a separate offline comparison sheet.
See the [capture interaction design](../../docs/design/request-capture.md#interaction-and-sensitive-presentation)
for the layout and help behavior.
Preview runs the existing chain locally against original complete snapshots;
save uses the CLI version check and activation transaction. A failure or conflict
retains the draft. Incomplete originals can derive metadata rules without an
invented preview body.

Captured URLs, query parameters, Authorization, Cookie and body values are shown,
searched and copied directly. Native archives and HAR exports include this
original content and must be kept private. Export and import use the CLI;
imported sessions are stopped and read-only. History stays in memory unless
explicitly exported. Ordinary errors do not include captured content.

Hiding, minimizing and sleeping cancel reads. Returning to the window resumes
queries immediately without starting or resuming recording. Closing the window
stops only a session started by that App window and retains history; a session
started from the CLI survives window closure. App quit stops its own session
before the existing quit cleanup. Profile/backend changes or TUN/engine exit stop
capture, and later restart does not resume it. Settings operations, capture
mutations and quit cleanup share the App operation gate.

The native source has model, transport and window lifecycle tests. The toolbar
and scope popover changes have not been compiled or interactively verified on
the Linux authoring host; native macOS build, tests and interaction remain pending.
Actual Linux/macOS TUN and helper fixtures, native trust cleanup and the
model-to-CLI capture-to-rule chain have verification evidence. Manual menu-bar
clicks and sleep/wake acceptance remain unverified. The updated table/detail
layout and its new regression tests also require macOS build, test and
interactive verification; they were authored and source-reviewed on Linux.
The quick-rule layout update also awaits native macOS compilation and interaction
checks.
See the [capture guide](../../docs/guides/request-capture.md),
[shared design](../../docs/design/request-capture.md) and
[verification record](../../docs/development/request-rewrite-acceptance.md#capture-verification).

Dialog text fields use AppKit's standard editing commands: **⌘V** to paste, **⌘C** to copy, **⌘X** to cut, **⌘A** to select all, **⌘Z** to undo, and **⇧⌘Z** to redo when available. The app installs an Edit menu and routes these commands to the focused input, including the editable provider group selector. After changing this menu, verify shortcuts on macOS in both Add Provider… and Add and Apply Subscription…, including after a validation error keeps a dialog open.

The top of the status menu contains one summary row. Errors, including an enabled TUN whose adapter is absent or down, show **Nagi · Error** with a trailing `exclamationmark.triangle` SF Symbol; ordinary operation notices show **Nagi · Info** with a trailing `info.circle` SF Symbol when there are no errors. Both symbols use the same 14-point size and baseline alignment. Its submenu contains only current error and notice text, without repeated icons or an engine-state row. Detail text wraps within a 320-point row without truncation. These messages do not appear as separate rows in the main menu. With no messages, the summary shows the engine state directly. Notices keep their existing lifetime: they have no dismissal timer and are cleared when another relevant operation begins. The Subscriptions menu offers Add and Apply Subscription… to prompt for a name, URL, update interval, and optional latency test URL, save the record and these settings, download and validate its cache, and apply it in sequence. The update interval accepts `off`, 1 through 31,536,000 seconds, and common choices of 1, 6, or 24 hours. Each saved subscription shows quota, expiry when available, next update or scheduler status, and any pause or failure, followed by **Update and Apply**, **Update Settings…**, **Latency Check Settings…**, and **Delete Subscription…**. Provider and subscription download intervals remain separate from automatic latency check intervals. A failed download prevents apply and retains the saved record; a failed apply retains the new cache and reports that activation failed. Retry Update and Apply. For an encrypted feed, set its identity file with `nagi subscription key NAME FILE` in the CLI first, then retry in the app. The Update and Apply tooltip and creation prompt disclose that apply updates the subscription, retains local settings and nodes, and clears saved selections. These operations do not implicitly start mihomo or enable traffic access. Ordinary CLI subscription preview, download-only update, and cached apply remain available. Deleting a subscription removes its record and cache after confirmation and leaves any previously applied profile intact. Mihomo Selector, URLTest, and Fallback groups offer clickable node choices. Automatic groups show their current result or fixed choice and offer Restore Automatic Selection; other group types show their members as read-only items. Groups with no members show an explicit message. A TUN setting can be on while its adapter is absent or down; the status summary includes that warning in its submenu. Under “Login,” “Run engine at login” installs or uninstalls the CLI's login service; uninstalling removes its service definition and leaves profiles and settings intact. “Open menu bar app at login” uses macOS `SMAppService` and requires an installed app bundle.

Subscription expiry and next-update times use the system's local time zone and `YYYY-MM-DD HH:mm`, without seconds or a zone suffix. Invalid timestamps display `unknown`; a missing next-update time displays `pending`, and a missing expiry is omitted. Quota shows available/total plus uploaded/downloaded amounts in binary units (`B`, `KiB`, `MiB`, `GiB`, `TiB`, `PiB`, `EiB`) with up to two decimal places. Available quota is clamped to zero; an absent or nonpositive total displays `unknown`. These values come from the provider's latest subscription metadata, not live traffic. This display formatting does not change CLI JSON fields or stored metadata.

Proxy node measurement tooltips show the measurement time in the system's local time zone as `YYYY-MM-DD HH:mm`. Invalid or absent times display `unknown`.

Build and package on macOS with Xcode command line tools:

```sh
make build app
open bin/Nagi.app
```

`make build app` calls the packaging script on macOS and creates `bin/Nagi.app` with `LSUIElement` set to true. You can also run `macos/NagiApp/package-app.sh` directly to rebuild only the App bundle. SwiftPM alone builds an executable, not an app bundle with the menu-only activation setting. Install the built app in Applications before enabling its login item; include the bundle in the [artifact directory](../../docs/development/install.md) to install it alongside the CLI. The app is not signed or notarized by this script. `make release` includes it in the matching macOS archive when the bundle exists on the release host.

About Nagi displays the version derived from the Git checkout, with the leading `v` removed. Release metadata and the cloud checks that keep App and CLI versions consistent are defined in the [release process](../../docs/development/release.md#version-numbers).

## Icon resources

The Calm Current icon family uses a continuous N for the menu bar and a teal-to-blue application tile for Finder and About. Runtime resources and editable artwork have separate locations:

| Location | Purpose |
| --- | --- |
| [`Resources/Nagi.icns`](Resources/Nagi.icns) | Application icon copied to `Nagi.app/Contents/Resources` and selected by `CFBundleIconFile`. |
| [`Sources/NagiApp/Resources/NagiMenuTemplate.pdf`](Sources/NagiApp/Resources/NagiMenuTemplate.pdf) | Vector menu-bar template copied into SwiftPM's `NagiApp_NagiApp.bundle`. |
| [`Artwork/source/`](Artwork/source/) | Editable SVG sources for the application and menu-bar artwork. |
| [`Artwork/app/`](Artwork/app/) | 1024-pixel PNG and the complete macOS `AppIcon.appiconset` export for future asset-catalog use. |
| [`Artwork/menubar/`](Artwork/menubar/) | Transparent black menu-bar PNG exports at 1×, 2×, and 3×. |
| [`Artwork/nagi-icon-preview.png`](Artwork/nagi-icon-preview.png) | Design preview; displayed traffic values are illustrative. |

The packaging script includes the SwiftPM resource bundle under `Contents/Resources`. `StatusItemImage` resolves that packaged bundle before falling back to `Bundle.module` for SwiftPM builds, so an installed app does not depend on the original build directory. Only the ICNS and menu-bar PDF are packaged; editable sources, previews, and alternate exports remain in the repository. When changing the artwork, update its SVG source and the corresponding exports together. Verify both Display choices, light and dark menu bars, Retina rendering, idle states, and the applying-setting animation on macOS; a build alone does not verify their appearance.
