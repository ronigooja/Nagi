# Project Documentation Guidelines

These guidelines apply to creating, organizing, and maintaining documentation in the Nagi repository.

## Documentation Structure

```text
README.md               Project entry point and documentation navigation
AGENTS.md               Documentation organization and maintenance rules for agents
ARCHITECTURE.md         System architecture overview
docs/
├── guides/             User-facing task guides
├── reference/          Precise command, configuration, and interface specifications
├── design/             Module design, interaction flows, and failure handling
├── development/        Build, test, debugging, mihomo integration, and contribution workflows
└── decisions/          Important architecture decisions and their rationale
```

This structure defines where content belongs. Directories do not need to be created in advance: create a document directory only when it contains actual content, and do not create empty directories or placeholder documents.

## Responsibilities and Boundaries

| Location | Question answered | Content scope |
| --- | --- | --- |
| `README.md` | What is the project, and where do I start? | Project purpose, supported platforms, current capabilities, shortest getting-started path, and documentation navigation |
| `AGENTS.md` | How should agents organize and maintain documentation? | Documentation categories, locations, references, and maintenance rules |
| `ARCHITECTURE.md` | How is the overall system organized? | System relationships, repository boundaries, module responsibilities, dependency direction, key constraints, and links to detailed designs |
| `docs/guides/` | How do I complete a user task? | Prerequisites, steps, expected results, and common issues |
| `docs/reference/` | What exactly is a command, configuration item, or interface? | Parameters, fields, types, defaults, constraints, exit codes, and compatibility rules |
| `docs/design/` | How does a mechanism work? | Component cooperation, data flow, state transitions, concurrency, failure handling, and recovery |
| `docs/development/` | How do I develop and maintain the project? | Development environment, build, testing, debugging, mihomo integration, contribution, and release workflows |
| `docs/decisions/` | Why was this architecture choice made? | Decision context, alternatives, selected approach, trade-offs, and consequences |

System architecture is documented in `ARCHITECTURE.md` and `docs/design/`. `AGENTS.md` defines documentation rules and links to the system design rather than duplicating it.

## Agent Workflow Rules

- Before making changes, read this file, `README.md` (if present), and the architecture, design, and reference documents directly related to the task.
- Classify the content before choosing a destination; do not move documents merely because the implementation phase has changed.
- When changing functionality, interfaces, configuration, or development workflows, check for the relevant authoritative document and update it once the behavior is settled.
- Do not describe a recommended architecture in `ARCHITECTURE.md`, an unimplemented design, or an alternative in a decision record as a current capability.
- When it is unclear whether behavior is implemented, use the current code, tests, and reproducible command results as evidence. If it cannot be verified, mark the status explicitly instead of guessing.
- After making changes, check new or modified relative links, commands, paths, field names, and examples. Run existing documentation checks, builds, or tests when applicable.
- Agents may modify files only within the scope authorized by the user. Do not rewrite unrelated documentation or implementation as part of the task.

## Organization and References

- `README.md` is the unified documentation entry point and links to the architecture overview and the main entry points for each documentation category.
- When documents conflict, prefer the more specific document and the verified current implementation. Resolve the conflict by updating stale references; do not leave two documents asserting different conclusions.
- Each topic must have one authoritative document containing the complete definition of its parameters, fields, or behavior. Other documents should contain only summaries, examples, and relative links to that source.
- `ARCHITECTURE.md` maintains an overall view and links to detailed designs instead of accumulating command parameters and operational steps.
- Guides may include examples needed to complete a task; complete parameter and configuration definitions belong in reference documentation.
- Design documents describe the currently accepted design; decision records preserve the reasons for choosing it.
- Organize by content responsibility, not by development phase.
- Each document should cover one coherent topic. Split only when content needs to be read or maintained independently; avoid excessive fragmentation.
- Files under `docs/` use short, clear, lowercase English names with hyphens between words. Links use repository-relative paths.
- When a category contains enough documents that navigation becomes unclear, add a `README.md` index containing only navigation and brief descriptions.

## Writing Rules

- Write prose in English by default; preserve the original spelling of code identifiers, commands, and API fields.
- State the document topic and scope at the beginning, then organize content in the order readers need to complete the task or understand the system.
- Clearly distinguish implemented behavior, accepted but unimplemented designs, and proposals under discussion. Guides and reference documents must not present planned capabilities as current functionality.
- Design documents must explain responsibilities, interfaces, key flows, and the state after failures. Diagrams must agree with the surrounding text.
- Name architecture decisions `NNNN-topic.md`, using four-digit decimal numbers starting at `0001` in creation order. Never reuse a number.
- Each decision must record its status, context, alternatives, decision, and consequences. Status must be one of proposed, accepted, superseded, or deprecated. When a decision is superseded, keep the original record and link to the replacement.
- Mark planned architecture and unimplemented designs with their status and scope at the beginning of the document. Update the description to current behavior only after implementation is complete.
- Examples must be accurate and self-consistent. Mark placeholder parameters clearly, and never include real credentials or subscription URLs.
- Prefer relative paths, environment variables, or generic examples. Local paths such as `/root/Nagi` and `/root/mihomo` may appear only as explicitly labeled development examples, never as architecture constraints.

## Maintenance Rules

- Once functionality, interface, or development workflow behavior is settled, update affected documentation in the same change. Experimental implementations may remain outside formal guides and reference documents, but must not be presented as stable capabilities.
- When the design changes, update its authoritative source. Record important architectural trade-offs in a decision record.
- When adding, moving, or deleting a document, update navigation and all affected links.
- Verify that commands, paths, fields, and examples in the documentation match the described implementation or design.
- Remove duplicate or obsolete explanations. Preserve historical rationale in decision records instead of current usage documentation.
- At minimum, documentation changes must verify Markdown relative links and example paths. Changes involving commands, configuration, or interfaces must also verify parameters, fields, exit codes, and example output against the current implementation.
- If the repository has no automated documentation checks, perform a manual review before submission and record any checks that could not be run and why.
