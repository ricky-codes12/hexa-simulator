# AGENTS.md

This is the required entry point for every human or coding agent working in
**hexa-simulator**. Read it before proposing or changing code.

## Agentic engineering model

This project uses **human + coding agent + Hexa.Build** as its engineering loop.
The coding agent may be ChatGPT/OpenAI—such as the assistant used with Hexa.Build
in the current engineering workflow—or another capable agent. This repository must
remain safe for a different coding agent to continue later. Durable authority lives in repository contracts,
documentation, accepted Git state, and Hexa.Build evidence—not in chat history.

The coding agent is a co-engineer, not the delivery authority. It may inspect,
reason, design, implement, test, and prepare canonical Change Proposals. Hexa.Build
owns exact-base validation, durable intake/run evidence, SCM delivery, accepted-main
proof, immutable Runtime orchestration, and qualified release operations.

## Read first

1. HEXA_BUILD_PROJECT.md
2. docs/PROJECT_OPERATING_CHARTER.md
3. docs/engineering/DEVELOPMENT.md
4. docs/engineering/TESTING.md
5. docs/engineering/DELIVERY.md
6. .hexa/project.yaml
7. .hexa/project
8. docs/architecture/ARCHITECTURE.md
9. docs/DEMO_FLEET.md (the demo fleet, wiring Hexa.Sensor, plan status)

Add project-specific architecture, decisions, state, handoff, and validation
documents to this list as the project matures.

## Non-negotiable rules

- Never bypass .hexa/project with hidden lifecycle logic in an agent prompt.
- Never mutate accepted main through automation outside the governed SCM path.
- Never treat a human checkout as Runtime state or automation scratch space.
- Never put secrets in prompts, commits, command arguments, logs, Change Proposals,
  or durable evidence.
- Never edit Hexa.Build SQLite state, Agent JSON, systemd units, or Runtime release
  directories directly when a supported Hexa.Build operation owns that change.
- Keep project-specific setup, verification, development, Runtime, and tool-cache
  behavior in this repository; keep machine policy in Hexa.Build.
- Treat HEXA_PROJECT_CACHE_DIR as the project-scoped writable cache authority;
  map tool-specific caches below it instead of weakening host sandboxing.
- Runtime v1 is implemented by .hexa/project for this starter. Keep release build, verification, activation, health, logs, and stop behavior project-owned and fail closed.
- Preserve exact accepted Git revision as delivery/Runtime source authority.
- Record rationale, assumptions, risks, evidence, and next actions in durable docs.

## Working loop

Before implementation:

1. Confirm the accepted baseline and clean worktree.
2. Read the relevant project docs and contracts.
3. Perform an architecture pre-mortem across project, Hexa.Build, SCM, Runtime, and
   host-security boundaries.
4. Identify what the change enables, what it mutates, and how it rolls back.

During implementation:

1. Keep the change cohesive and project-owned.
2. Add tests for invariants and failure paths, not only happy paths.
3. Run project-owned checks through .hexa/project/Hexa.Build where practical.
4. Do not silently introduce ambient environment or host assumptions.
5. Update durable documentation when architecture or operating behavior changes.

Before delivery:

1. Run the project's canonical verification.
2. Ensure git diff --check is clean.
3. Build a canonical hexa.build/change-proposal/v1 against the exact accepted base
   when using agentic delivery.
4. Let Hexa.Build validate in a disposable workspace and own SCM submission/merge.
5. Verify accepted-main and Runtime state after delivery.

## Human authority

The human Product Owner/developer remains the final acceptance authority for
product intent and release decisions. A coding agent should challenge unsafe or
inconsistent instructions, explain material tradeoffs, and never claim evidence it
did not actually observe.

## Continuity

Do not rely on private scratchpads or chat memory as project state. Keep a concise
current-state/handoff record in repository documentation once the project becomes
non-trivial, so a future human or coding agent can continue safely without needing
this conversation.
