# Security Policy

## Supported versions

Cairn is pre-release. Security fixes are applied to the `main` branch. Tagged releases will be listed here once versioning begins.

| Version | Supported |
|---------|-----------|
| main    | Yes       |

## Reporting a vulnerability

If you find a security issue in Cairn, please report it responsibly.

**Preferred:** open a [GitHub private security advisory](https://github.com/jackalgg/cairn/security/advisories/new) on this repository.

**Alternative:** email the maintainer with the subject line `Cairn security report`. *(Add a contact address before public launch.)*

Please do not open a public GitHub issue for security vulnerabilities.

### What to include

- A description of the issue and its potential impact
- Steps to reproduce (commands, flags, sample YAML — redact secrets)
- Cairn version or commit hash
- Your environment (OS, Go version) if relevant

### What to expect

- Acknowledgment within 7 days
- A fix or mitigation plan within 30 days for confirmed issues
- Coordinated disclosure once a fix is available

Timelines may vary for pre-release software. We will communicate status even when a fix is not yet ready.

## Scope

### In scope

- Local file read/write behavior (`scan`, `fix`, `compat`)
- Path handling for `--out` and in-place writes
- YAML parsing limits and denial-of-service vectors
- Credential handling when using `--cluster`, `--kubeconfig`, or `--context`
- Incorrect or unsafe auto-fixes that weaken manifest security

### Out of scope

- Vulnerabilities in upstream dependencies (report to the upstream project; we will bump versions)
- Kubernetes cluster security issues unrelated to Cairn's local operation
- Issues in manifests Cairn did not generate or modify

## Known architectural risks

Cairn reads and writes local YAML files and optionally reads kubeconfig to probe cluster version. It does **not** apply changes to a live cluster today.

Known risks tracked in the project backlog:

- In-place file overwrite without atomic rename or backup
- Path traversal potential in `--out` directory writes
- Unbounded YAML input size
- YAML round-trip rewrites that alter file content beyond the intended fix

See the [Known limitations](README.md#known-limitations-todos-and-audit-findings) section in README.md for the full list.
