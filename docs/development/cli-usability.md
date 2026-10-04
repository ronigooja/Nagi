# CLI usability rules

This document defines development and review rules for Nagi's command-line interaction. Apply these rules when adding or changing commands. The [CLI reference](../reference/cli.md) defines implemented syntax and compatibility; these rules do not imply that a proposed feature already exists.

## Help and command discovery

- Provide top-level help and help for each command and subcommand, including syntax, purpose, arguments, relevant defaults, and a useful example.
- Make help available without a running engine, a valid profile, or an installed mihomo binary. Reading help must not change application state.
- On missing or invalid arguments, identify the problem and show the applicable usage. Suggest a discovery command when users need an existing profile, group, or node name.
- Keep command names and argument conventions consistent. Examples containing spaces must quote arguments correctly; placeholder values must be recognizable.

## Output for people and programs

- Design default output for terminal reading: put the result or state first, label important values, and mark current selections in lists.
- Print requested log lines and configuration text directly. Do not wrap them in JSON or add prose to nonempty raw content.
- Explain empty lists and unavailable information. Do not imply that an empty result is an error or that a missing value was verified.
- Keep human formatting separate from the JSON interface. Preserve documented JSON fields, envelope, output streams, and exit codes when changing presentation. Scripts should use `--json`.
- Send failures to stderr and successful results to stdout. Help requested for a valid command must succeed. Usage errors must include corrective guidance.

## Results, failures, and next steps

- State what an operation actually completed. Distinguish saving a subscription, downloading its cache, importing a profile, selecting it, and running the engine.
- Give a concrete next command when another step is necessary. Do not equate a running process with usable proxy nodes or configured system proxy settings.
- Explain failures with enough context to act: what failed, the known resulting state, and a relevant check or recovery step. Do not assert rollback or successful activation without evidence.
- Keep credentials and subscription URLs out of ordinary diagnostics. If engine diagnostics cannot be safely exposed, explain how to inspect the relevant configuration or run validation locally; do not direct users to a deleted temporary file.
- Keep noninteractive commands usable by scripts. Any future interactive prompt must define behavior for redirected input and provide an explicit noninteractive path.

## Review and verification

For affected behavior, verify help, missing and invalid arguments, normal and empty output, and actionable failures. Test help with unavailable or invalid runtime configuration. Check JSON success and failure compatibility independently of text formatting. When validation changes, verify that diagnostics do not reveal sensitive configuration values.

Update the CLI reference and affected guides in the same change. Test representative user journeys rather than only matching renderer internals. Use isolated runtime directories for checks that change profiles, subscriptions, or process state; see [Build and test](build-and-test.md).

## Integration maintenance

When adding a CLI command, update its `commandSpec` for help, argument count, and completion discovery; add value checks before runtime resolution. Preserve the `ok` envelope and existing JSON field names. Add human formatting separately from JSON. If the command accepts a local profile or subscription name, extend the generated shell completion only after a read-only local candidate source exists. Do not put credentials, subscription URLs, or engine validation output in errors; the output layer also redacts URL-shaped text as a final boundary. Verify help with invalid runtime configuration, text and JSON success, JSON error code and exit status, and generated Bash/Zsh/Fish scripts.
