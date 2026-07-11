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

Cairn is a local command-line tool that reads YAML and rewrites its indentation.
It does not talk to a network, a cluster, or any credentials.

### In scope

- Local file read/write behavior (`cairn fix`, including `--in-place`)
- YAML parsing limits and denial-of-service vectors (e.g. very large or deeply nested input)
- Any case where a repair alters more than leading whitespace (see below)

### Out of scope

- Vulnerabilities in upstream dependencies (report to the upstream project; we will bump versions)
- Correctness of a manifest's *content* — Cairn does not validate, lint, or apply manifests
- Issues in YAML Cairn did not modify

## Known architectural risks

Cairn reads and writes local YAML files only. Risks tracked in the backlog:

- **In-place overwrite** happens directly via `os.WriteFile`; a crash mid-write could truncate a file. Atomic write (temp file + rename) is planned. Use `--dry-run` or version control if this matters to you.
- **Unbounded input size** — no size or nesting-depth limit on YAML input yet.
- **Content-preservation is not yet enforced as a hard invariant.** By design Cairn only changes leading whitespace, but there is not yet a guard that *refuses to write* if a repair changed anything else. Adding that guard is on the roadmap. Until then, review `--dry-run` output for anything important.

See the [Roadmap](README.md#roadmap) in README.md for planned mitigations.
