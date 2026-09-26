# Delivery

Future changes are normal canonical hexa.build/change-proposal/v1 artifacts against exact accepted source. GitLab CI and GitHub Actions are thin wrappers around project-owned setup/verification. Runtime is optional and may activate only from accepted source; its external database configuration is supplied separately through protected Runtime environment authority. Production database migration/deployment remains a project-owned concern and is not hidden inside generic Hexa.Build Runtime build/activation.

Runtime configuration: `.hexa/project.yaml` explicitly passes `RUNTIME_ENV_FILE` to the adapter. Hexa.Build supplies the protected file path from the registered Runtime policy; secrets remain outside Git. Without this allowlist entry, activation exits 78 even when the environment file is configured.
