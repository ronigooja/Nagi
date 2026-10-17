# Proxy providers

This guide explains how to add a node source to the current profile and use its nodes through a proxy group. The [CLI reference](../reference/cli.md#adding-proxy-providers) defines input constraints, defaults, persistence, and failure behavior.

## Before adding

Prepare an HTTP(S) subscription URL that mihomo can decode and select an existing personal profile. You need the matching mihomo executable for configuration validation, but the engine may be stopped. For an existing profile, run `nagi profile use NAME`, replacing `NAME` with its name. The built-in `default` profile is replaced during installation and updates; use a personal profile for settings you maintain.

## Add from a menu

In the terminal, run `nagi menu`, open **Select Node → Proxy Providers → Add provider**, and enter a name, subscription URL, target proxy group, and update interval. The default interval is one hour. The Add action remains available when the provider list is empty or cannot be read.

In the macOS app, open **Proxy Providers → Add Provider…** and complete the same fields. Choose an existing supported group or enter a new group name. The app calls the CLI to save and activate the change; it does not edit configuration itself.

Successful creation updates the current profile's local override and reloads a running engine. If it was stopped, start it separately with `nagi start`. Configuration validation or activation failures preserve the previous disk settings; the command reports any uncertainty about runtime recovery. Correct the error before explicitly retrying.

Providers added to automatic groups use the group's healthcheck URL. After reloading a running engine and loading the provider nodes, Nagi immediately requests mihomo's native healthcheck without separate result output. This reduces fallback to entries such as `REJECT` while nodes lack measurements; unavailable nodes can still cause fallback.

## Add from the CLI

Replace the example URL with your own subscription URL and use the desired target group:

```sh
nagi proxy provider add airport 'https://example.com/subscription' 'My Proxy' 3600
nagi proxy providers
nagi proxy show 'My Proxy'
```

The list and group inspection commands require a running engine. Omitting `3600` uses the same default interval. An existing group gains this provider's nodes; a missing group creates a manual select group that initially selects `DIRECT`. Adding a group leaves routing rules and runtime mode unchanged. Use rules already targeting the group, or switch to global mode and choose the group in `GLOBAL`:

```sh
nagi mode global
nagi proxy select GLOBAL 'My Proxy'
```

Then select a provider node from the group's **Select Node** menu (**Proxy Groups** in the macOS app), or use `nagi proxy select 'My Proxy' 'NODE_NAME'`, replacing `NODE_NAME` with a listed node. Clicking a node inside the terminal Provider detail page measures latency; selection happens in the proxy group.

## Refresh and maintain

Use **Update provider** to reload its node source and **Start healthcheck** to trigger mihomo's native checks. The corresponding commands are:

```sh
nagi proxy provider update airport
nagi proxy provider healthcheck airport
```

Provider node lists in the terminal menu show the latest measured latency, failure, untested, or unknown state. Group node lists in both interfaces show the same engine observations; opening a menu does not measure nodes. In the App, use **Proxy Groups → GROUP → Test Single Node** for an individual check, or the group's batch-test action. Automatic-group batch tests disclose that they restore automatic selection.

To inspect or change the automatic check interval, open **Automatic latency checks** in the terminal provider details, or **Healthcheck Settings…** in the App provider submenu. The CLI equivalents are:

```sh
nagi proxy healthcheck show provider airport
nagi proxy healthcheck set provider airport 600
nagi proxy healthcheck reset provider airport
```

These settings affect latency checks separately from provider downloads. Saving an interval retains the provider's enable setting; lazy scheduling can skip unused nodes. The [interval reference](../reference/cli.md#automatic-latency-check-intervals) defines constraints, scheduling caveats, and restore behavior.

Provider updates refresh nodes within the current configuration. Nagi's subscription download and apply operations manage whole profile configurations instead. Explicit and automatic subscription applications retain locally added providers and group attachments while rebasing node/group changes against the new feed. Clearing or replacing the profile's override can remove these additions. See the [persistence contract](../reference/cli.md#adding-proxy-providers) before combining the two workflows.

## Remove a provider

Open a provider's details in `nagi menu` and choose **Delete provider**, or choose **Proxy Providers → PROVIDER → Delete Provider** in the macOS app. The terminal returns to the provider list only after successful deletion. From the CLI:

```sh
nagi proxy provider remove airport
```

Deletion removes its definition and group `use` references from the selected profile and local override, then validates and reloads if running. It retains groups and routing rules. It also works while stopped. A failed deletion restores the previous settings. If other fields reference individual nodes supplied by the provider, update those references before retrying. A later subscription replacement can restore a provider originally supplied by that subscription. See the [removal contract](../reference/cli.md#removing-proxy-providers) for persistence and failure behavior.
