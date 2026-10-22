# CLI usability rules

This document defines development and review rules for Nagi's command-line interaction. Apply these rules when adding or changing commands. The [CLI reference](../reference/cli.md) defines implemented syntax and compatibility; these rules do not imply that a proposed feature already exists.

## Help and command discovery

- Provide top-level help and help for each command and subcommand, including syntax, purpose, arguments, relevant defaults, and a useful example.
- Make help available without a running engine, a valid profile, or an installed mihomo binary. Reading help must not change application state.
- On missing or invalid arguments, identify the problem and show the applicable usage. Suggest a discovery command when users need an existing profile, group, or node name.
- Keep command names and argument conventions consistent. Examples containing spaces must quote arguments correctly; placeholder values must be recognizable.

## Output for people and programs

- Design default output for terminal reading: put the result or state first, label important values, and mark current selections in lists.
- Follow the shared [local output and sensitive-content rules](ui-design.md#local-output-and-sensitive-content) for text, JSON, errors, and menu presentation.
- Print requested log lines and configuration text directly. Do not wrap them in JSON or add prose to nonempty raw content.
- Explain empty lists and unavailable information. Do not imply that an empty result is an error or that a missing value was verified.
- Keep human formatting separate from the JSON interface. Preserve documented JSON fields, envelope, output streams, and exit codes when changing presentation. Scripts should use `--json`.
- Color recognized status values in human-readable CLI summaries only on a capable terminal. Keep JSON and redirected output plain, and preserve raw configuration, logs, and live streams byte for byte.
- Send failures to stderr and successful results to stdout. Help requested for a valid command must succeed. Usage errors must include corrective guidance.

## Results, failures, and next steps

- State what an operation actually completed. Distinguish saving a subscription, downloading its cache, importing a profile, selecting it, and running the engine.
- Give a concrete next command when another step is necessary. Do not equate a running process with usable proxy nodes or configured system proxy settings.
- Explain failures with enough context to act: what failed, the known resulting state, and a relevant check or recovery step. Do not assert rollback or successful activation without evidence.
- Preserve useful field paths, error codes, and causes from engine validation diagnostics. Prefer structured diagnostics and omit unrelated credentials while retaining operational context. If useful details are unavailable, explain how to inspect the relevant configuration or run validation locally; do not direct users to a deleted temporary file.
- Keep ordinary commands usable by scripts. The terminal menu requires terminal input and output, rejects `--json`, and directs redirected or automated use to ordinary commands and their JSON results.

## Terminal menu interactions

- Follow the shared [UI design rule](ui-design.md) for interface wording and presentation.
- Keep pages line-oriented and preserve scrollback. Show the title, a state summary or explicit unknown/read error, resource choices, fixed actions, and navigation.
- Keep styling within the menu presentation layer and follow the [menu style contract](../reference/cli.md#terminal-menu). Detect output TTY, `TERM`, and `NO_COLOR`; plain mode must retain readable labels and selected markers. Carry actual node/profile selection and feedback severity as semantic state, apply the shared content-visibility rules before styling, and reset every styled span before input or other output. Do not add styles to requested raw content or live streams.
- Use numbers for every selectable entry: up to ten resources, then actions, then pagination entries, consecutively numbered from a shared display/dispatch mapping. Use `0` for Back, Cancel, or Exit at home. Do not add letter shortcuts, refresh, filtering, search, or help entries. Reload state on entry, after operations, and on return; paginate the existing snapshot. Clamp invalid page indices after reload. Blank or invalid input repeats only the prompt.
- Dispatch actions once without a second confirmation.
- Pass known resource names as single command arguments. Text prompts treat `0/b/q` literally. Ctrl-C cancels a form or returns to the parent page, and exits at home; EOF exits. Use one session input mechanism without abandoned readers.
- Validate required fields locally, retaining other values. Preserve form values after command failure and offer numeric editing, resubmission, and cancellation; never retry a mutation automatically.
- Keep direct selection consistent in ordinary and latency node lists. Show automatic/fixed group state and disclose that latency testing may release fixed selection. Preserve partial-completion and unknown-state errors.
- Daily subscription actions combine download and apply through existing commands; creation saves the record first. Do not apply after a failed download or repeat creation after a later-stage failure. Show concise metadata and completion feedback without automatic previews or change counts. Keep download-only, cached apply, and preview available through ordinary commands. Cache validation and activation remain command responsibilities.
- Use existing commands for reads and mutations. Keep menu presentation separate from ordinary CLI and JSON contracts; preserve raw configuration/log output and useful errors and resource context. Clear stale choices after failed reads and leave recovery actions accessible.
- Show success and failure feedback after the reloaded page and before its prompt. Pause raw configuration, override, profile comparison, and recent log output until Enter before returning to the page.
- Cancel child log and traffic streams with Ctrl-C and return to the same page. Do not promise cancellation of an ordinary operation already submitted.

## Review and verification

For affected behavior, verify help, missing and invalid arguments, normal and empty output, and actionable failures. Test help with unavailable or invalid runtime configuration. Check JSON success and failure compatibility independently of text formatting. When validation changes, verify useful local error details, omission of unrelated credentials, and redaction of diagnostic reports intended for sharing. For menu changes, also check redirected input and output, `--json menu`, numeric navigation, pagination, single-dispatch actions, retained forms, stale or empty data, and interrupted streams. For styling changes, verify enabled and plain output, empty or `dumb` `TERM`, nonempty `NO_COLOR`, selected markers, feedback classes, content visibility, raw-output preservation, and resets before input and after interruption.

Update the CLI reference and affected guides in the same change. Test representative user journeys rather than only matching renderer internals. Use isolated runtime directories for checks that change profiles, subscriptions, or process state; see [Build and test](build-and-test.md).

## Integration maintenance

When adding a CLI command, update its `commandSpec` for help, argument count, and completion discovery; add value checks before runtime resolution. Preserve the `ok` envelope and existing JSON field names. Add human formatting separately from JSON. If the command accepts a local profile or subscription name, extend the generated shell completion only after a read-only local candidate source exists. Verify help with invalid runtime configuration, text and JSON success, JSON error code and exit status, and generated Bash/Zsh/Fish scripts.
