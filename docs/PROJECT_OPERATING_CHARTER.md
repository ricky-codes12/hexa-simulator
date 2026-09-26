# Project Operating Charter

## Mission

hexa-simulator is a Hexa.Build-native project. Project-specific engineering behavior belongs
in this repository; Hexa.Build owns delivery/runtime orchestration and durable
machine authority.

## Authority boundaries

- Accepted Git state: source truth.
- .hexa/project.yaml: project identity and minimal environment contract.
- .hexa/project: executable project lifecycle contract.
- Hexa.Build: Change validation, Agent delivery, evidence, Runtime desired state,
  immutable release authority, setup, diagnostics, backup/recovery.
- SCM provider: remote repository/pipeline/merge authority through a modular
  provider boundary.
- Human: product intent and final acceptance.
- Coding agent: bounded engineering collaborator operating through these contracts.

## Evolution

Add project-specific architecture decisions and explicit acceptance evidence as
capabilities are introduced. Do not expand YAML into a second orchestration DSL.
