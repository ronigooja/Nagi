# Project Agent Guidelines

These guidelines define development responsibilities and documentation practices for agents working in the Nagi repository.

## Architecture Boundaries and Feature Design

These rules apply to every feature:

- Treat mihomo as the implementation owner for proxy protocols, DNS resolution, traffic forwarding, routing rules, and TUN networking. Nagi configures and manages those capabilities through mihomo's configuration and APIs.
- Nagi owns user operations: translating intent into configuration or API calls, persisting settings, managing process lifecycle, displaying state, and handling operation failures.
- The macOS UI is the interactive entry point for common daily-use scenarios. The CLI remains the complete interface for advanced configuration, batch operations, automation, diagnostics, recovery, and operational tasks. The UI does not implement those business operations.
- Before designing a feature, inspect the pinned mihomo version's capabilities, configuration fields, and APIs. Identify the configuration or management work Nagi must provide.
- Use existing mihomo mechanisms instead of duplicating them in Nagi. If a capability is missing, determine whether the extension belongs in mihomo or Nagi according to the system boundary.
- Verify configuration generation, API calls, persistence, and activation behavior with relevant checks. Broader network detection or verification requires a separate requirement and must not become an implicit prerequisite for a configuration feature.
- For example, DNS leak prevention through mihomo means configuring the relevant TUN, DNS interception, and DNS upstream settings. It does not require Nagi to build a system-wide DNS leak verification mechanism.

## Documentation Locations and Responsibilities

```text
README.md               Project entry point and documentation navigation
AGENTS.md               Agent responsibilities and workflow rules
ARCHITECTURE.md         Current system architecture overview
docs/
├── guides/             User-facing task guides
├── reference/          Exact command, configuration, and interface specifications
├── design/             Current module designs, flows, and failure handling
├── development/        Build, test, debugging, integration, and contribution workflows
└── decisions/          Architecture decisions and their rationale
```

Create a documentation directory only when it contains actual content; do not create empty directories or placeholder documents.

Use each location as follows:

| Location | Scope |
| --- | --- |
| `README.md` | Project purpose, supported platforms, current capabilities, shortest getting-started path, and documentation navigation. |
| `AGENTS.md` | Agent responsibilities, documentation categories, workflow rules, and maintenance requirements. |
| `ARCHITECTURE.md` | Current system relationships, repository boundaries, module responsibilities, dependency direction, constraints, and links to detailed designs. |
| `docs/guides/` | Prerequisites, steps, expected results, and common issues for user tasks. |
| `docs/reference/` | Complete definitions of parameters, fields, defaults, constraints, exit codes, and compatibility rules. |
| `docs/design/` | Current component responsibilities, interfaces, data flow, state transitions, concurrency, failure handling, and recovery. |
| `docs/development/` | Development environment, build, testing, debugging, mihomo integration, contribution, and release workflows. |
| `docs/decisions/` | Context, alternatives, selected decision, trade-offs, and consequences for important architecture choices. |

`README.md` is the unified documentation entry point. `ARCHITECTURE.md` maintains the overall view and links to detailed designs; it must not accumulate command parameters or operational steps. Each topic has one authoritative document containing its complete definition. Other documents contain only summaries, examples, and relative links to that source. Organize documents by content responsibility, not development phase, and split a document only when its topic needs to be read or maintained independently.

## Agent Workflow

- Before making changes, read this file, `README.md` if present, and the architecture, design, and reference documents directly related to the task.
- Before modifying any file, explain the modification scope to the user in Chinese and request permission. Modify files only within the scope authorized by the user; do not rewrite unrelated documentation or implementation.
- Check `$HOME/environment` for required development tools before installing or downloading anything.
- Classify content before choosing a documentation destination. Do not move documents merely because the implementation phase has changed.
- When changing functionality, interfaces, configuration, or development workflows, check the relevant authoritative document and update it once behavior is settled.
- When changing CLI behavior, follow [the CLI usability rules](docs/development/cli-usability.md) and update [the CLI reference](docs/reference/cli.md) for implemented behavior.
- Do not describe a recommended architecture in `ARCHITECTURE.md`, an unimplemented design, or an alternative in a decision record as a current capability.
- When behavior is unclear, use current code, tests, and reproducible command results as evidence. If it cannot be verified, mark its status explicitly instead of guessing.
- After changes, check new or modified relative links, commands, paths, field names, and examples.

## Documentation Writing Rules

- Write documentation prose in English by default. Preserve the original spelling of code identifiers, commands, and API fields.
- State each document's topic and scope at the beginning, then organize content in the order readers need to complete the task or understand the system.
- Clearly distinguish implemented behavior, accepted but unimplemented designs, and proposals under discussion. Guides and reference documents must not present planned capabilities as current functionality.
- Design documents must explain responsibilities, interfaces, key flows, and the state after failures. Diagrams must agree with the surrounding text.
- Mark planned architecture and unimplemented designs with their status and scope at the beginning. Update the description to current behavior only after implementation is complete.
- Name architecture decisions `NNNN-topic.md`, using four-digit decimal numbers starting at `0001` in creation order. Never reuse a number. Each decision must record its status, context, alternatives, decision, and consequences. Status must be `proposed`, `accepted`, `superseded`, or `deprecated`. When superseded, keep the original record and link to the replacement.
- Keep examples accurate and self-consistent. Mark placeholder parameters clearly, and never include real credentials or subscription URLs.
- Prefer relative paths, environment variables, or generic examples. Local paths such as `/root/Nagi` and `/root/mihomo` may appear only as explicitly labeled development examples, never as architecture constraints.
- Use short, clear, lowercase English filenames with hyphens under `docs/`, and repository-relative links. When a category becomes difficult to navigate, add a `README.md` index containing only navigation and brief descriptions.

## Documentation Maintenance and Verification

- When functionality, an interface, configuration, or development workflow changes, update the affected authoritative documentation in the same change once behavior is settled. Experimental implementations may remain outside formal guides and reference documents, but must not be presented as stable capabilities.
- When the design changes, update its authoritative source and record important architectural trade-offs in a decision record.
- When adding, moving, or deleting a document, update navigation and all affected links. When documents conflict, prefer the more specific document and the verified current implementation, then update stale references so conflicting claims do not remain.
- Guides may include task-specific examples; complete parameter and configuration definitions belong in reference documentation. Design documents describe the currently accepted design; decision records preserve why it was chosen.
- Verify that commands, paths, fields, examples, and Markdown relative links match the implementation or design. Changes involving commands, configuration, or interfaces must also verify parameters, exit codes, and example output.
- Remove duplicate or obsolete explanations. Preserve historical rationale in decision records instead of current usage documentation.
- If the repository has no automated documentation checks, perform a manual review before submission and record any checks that could not be run and why.

## Communication

- Keep documentation in English unless the document is explicitly intended to preserve another language.
- During execution, communicate with the user in the language requested by the user. Progress reports and important information are communicated in Chinese unless the user requests otherwise.
