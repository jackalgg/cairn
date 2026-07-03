# Cairn

**Cairn** is a YAML repair tool. It scans any YAML file for the common mistakes that make manifests die in the terminal — tab indentation, inconsistent indentation, missing list markers, keys missing the space after a colon — and repairs them into a runnable format, reviewing each change with you interactively.

Kubernetes resources get an extra layer for free: when a document carries `apiVersion` and `kind`, Cairn also checks it for policy issues, deprecated APIs, and (with `--schema`) schema validity, and can harden workloads with Pod Security-style fixes. Plain YAML (CI configs, compose files, top-level lists) is checked and repaired for syntax only.

---

## What works today

Cairn reads YAML from a file, directory, or stdin, runs a stack of checks, and returns structured findings. Each finding says what's wrong, the line it's on, how serious it is, and whether Cairn can repair it. The `fix` command repairs the file and shows a diff before writing anything.

| Area | Status |
|------|--------|
| Generic YAML support (any file, not just manifests) | Done |
| Multi-doc YAML parsing (file, dir, stdin) | Done |
| Syntax repair: tabs, indentation, missing list markers, colon spacing | Done |
| Interactive repair review (accept / skip / all / quit) | Done |
| Apply verification via server-side dry-run (the `kubectl apply` path) | Done |
| Auto-repair loop: dry-run errors fed back into fixes until it applies | Done |
| Schema validation (kubeconform / OpenAPI, opt-in via `--schema`) | Done |
| Structure repair (unknown fields, missing required, type coercion) | Done |
| Policy rules: `runAsNonRoot`, `readOnlyRootFilesystem`, floating image tags | Done |
| API version deprecation detection + basic migration | Done (apiVersion only) |
| `scan`, `fix`, `compat`, `generate` commands | Done |
| kubectl error → fix mapping (`--from-error`, `--stdin-errors`) | Done |
| Cluster export generator (`cairn generate`) | Done |
| Optional cluster probe (`--cluster`) | Done (version + PSS namespace labels) |
| AI-assisted repair hook (`--ai`, `--accept-risk`) | Interface only |
| JSON output, dry-run diffs, `--out` writes | Done |

### How it flows

```mermaid
flowchart TD
    input[YAML input]
    syntax[Syntax detectors]
    k8scheck{apiVersion + kind?}
    k8s[Schema / policy / compat]
    findings[Findings]
    review["fix: interactive review"]
    output[Report or write]

    input --> syntax
    syntax --> k8scheck
    k8scheck -->|yes| k8s --> findings
    k8scheck -->|no| findings
    syntax --> findings
    findings --> output
    findings --> review --> output
```

Both `scan` and `fix` run the same syntax detection on every document. The Kubernetes checks only run when a document is actually a K8s resource. `scan` reports; `fix` repairs.

---

## Quick start

Requires Go 1.26+.

```bash
git clone https://github.com/jackalgg/cairn.git
cd cairn
go build -o cairn .

# Scan any YAML for syntax issues (tabs, indentation, list markers, colons)
./cairn scan testdata/repair/bad-tabs.yaml

# Repair a broken file interactively (review each change)
./cairn fix testdata/repair/bad-indent.yaml

# Non-interactive: accept all repairs (for CI/scripts)
./cairn fix --yes --in-place broken.yaml

# Pipe through stdin and get the repaired YAML on stdout
cat broken.yaml | ./cairn fix -

# JSON output for CI pipelines
./cairn scan --format json testdata/repair/missing-colon-space.yaml
```

[`test.yaml`](test.yaml) is an intentionally insecure Deployment (`nginx:latest`, no `securityContext`). The fixtures under [`testdata/repair/`](testdata/repair) cover tabs, bad indentation, missing colon spacing, and a non-Kubernetes config so you can see generic YAML support.

`scan` exits with a non-zero code when error-severity findings remain — useful in CI.

Run the test suite:

```bash
go test ./...
```

---

## Commands

| Command | Description |
|---------|-------------|
| `cairn scan [path]` | Scan any YAML for syntax issues (and K8s issues for manifests); print findings |
| `cairn fix [path]` | Repair YAML syntax interactively; apply K8s fixes; verify it would apply; write or diff |
| `cairn verify [path]` | Check whether manifests would be accepted by `kubectl apply` (server dry-run) |
| `cairn compat [path]` | Check and migrate deprecated API versions |
| `cairn generate` | Export, clean, and harden manifests from a live cluster |

`path` can be a file, a directory (all `.yaml`/`.yml` files), or `-` for stdin.

### Global flags

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | `human` | Output format: `human` or `json` |
| `--schema` | `false` | Validate K8s resources against OpenAPI schemas (needs network on first run) |
| `--kubernetes-version` | `1.30` | Kubernetes version for OpenAPI schema validation |
| `--target-version` | (same as above) | Target version for API compatibility checks |
| `--severity` | `warning` | Minimum severity to report: `error`, `warning`, or `info` |
| `--cluster` | `false` | Probe live cluster via kubeconfig |
| `--kubeconfig` | (default path) | Path to kubeconfig file |
| `--context` | (current context) | Kubeconfig context name |

### Fix flags

| Flag | Description |
|------|-------------|
| `--interactive` | Review each repair before applying (default; auto-disabled for piped stdin) |
| `--yes` | Apply all repairs without prompting (for CI/scripts) |
| `--verify` | Verify the repaired manifest would apply (default; server dry-run, or `--schema` offline) |
| `--max-repair-rounds` | Max verify/repair rounds when reconciling dry-run errors (default 3) |
| `--dry-run` | Show diff without writing files |
| `--out <dir>` | Write fixed files to a directory |
| `--in-place` | Overwrite source files (required for in-place writes) |
| `--fix-id <rule>` | Apply only K8s findings matching a rule ID (repeatable) |
| `--from-error <text>` | Match fixes to kubectl/admission error text |
| `--stdin-errors` | Read error text from stdin (pipe from `kubectl apply`) |
| `--repair-only` | Limit K8s repairs: `all`, `structure`, or `policy` |
| `--ai` | Enable AI-assisted repair after deterministic fixes |
| `--accept-risk` | Accept heuristic/AI repairs |

In interactive mode each proposed repair is shown with its line range, a
before/after preview, and a confidence level. Respond with `y` (apply), `n`
(skip), `a` (apply all remaining), or `q` (quit). Tab expansion is always a
high-confidence fix; indentation and list-marker guesses are heuristic, which is
exactly why they're shown for review.

Example reactive workflow:

```bash
kubectl apply -f test.yaml 2>&1 | ./cairn fix test.yaml --stdin-errors --dry-run
```

### Verifying fixes apply

`fix` and `verify` confirm that a manifest would actually be accepted by the API server using a **server-side dry-run** (`dryRun=All`) — the same admission, schema, and defaulting path `kubectl apply --dry-run=server` runs. Cairn talks to the cluster in your kubeconfig; nothing is created or modified.

```bash
# Just check whether manifests would apply (exits non-zero if not)
./cairn verify deploy.yaml

# Repair, then verify; if the dry-run is rejected, feed the error back into the
# fixer and retry until it applies or no further progress is possible
./cairn fix --yes --in-place deploy.yaml
```

When no cluster is reachable, pass `--schema` to fall back to offline OpenAPI validation (catches schema/shape errors but not admission webhooks). Disable verification with `--verify=false`. The reconcile loop is bounded by `--max-repair-rounds`.

This closes the loop that `--from-error`/`--stdin-errors` started: instead of pasting a kubectl error in by hand, Cairn produces the error itself via dry-run and applies the matching fix automatically.

### Cluster mode

With `--cluster`, Cairn connects to your kubeconfig, reads the server version, and enriches finding messages with cluster context. It does **not** modify the cluster, fetch live resources, or generate manifests from running workloads. That generator is planned — see [Roadmap](#roadmap).

Schema validation requires network access on first run (kubeconform downloads OpenAPI schemas from GitHub).

---

## Architecture

The CLI is thin; logic lives in `internal/`:

| Package | Role |
|---------|------|
| `internal/parser` | Multi-doc YAML parsing, generic syntax validation, GVK extraction |
| `internal/repair/syntax` | Line-oriented syntax detectors + repair proposals (tabs, indent, list markers, colons) |
| `internal/engine` | Orchestrates syntax → (K8s: validate → policy → compat) → findings |
| `internal/verify` | Confirms manifests would apply (cluster server-side dry-run, or offline schema) |
| `internal/schema` | kubeconform OpenAPI validation |
| `internal/policy` | Security rules with optional fix functions |
| `internal/compat` | Deprecated API version detection and migration |
| `internal/k8serrors` | kubectl/admission error string → rule mapping |
| `internal/cluster` | Optional live cluster probe |
| `internal/fixer` | Apply fixes, produce diffs, write output |
| `internal/report` | Human and JSON output |

Each policy rule implements `AppliesTo`, `Check`, and optionally attaches a `Fix` — a function that mutates the manifest. Detection and remediation stay linked so fix logic doesn't drift from detection logic.

Manifests are handled as Kubernetes `unstructured` objects rather than typed structs, which keeps the tool workable across many resource kinds and CRDs without a struct per kind.

---

## Roadmap

### Live cluster YAML generator

Cairn should eventually probe a live cluster — server version, enabled APIs, namespace Pod Security Standards labels — and produce manifests that are compatible with *that* environment. Fixed YAML gets written to disk; nothing is applied in-cluster.

Today the cluster probe only appends a version string to two finding messages. The generator will wire probed capabilities into schema validation and compat checks, and add something like `cairn generate` for cluster-aware manifest output.

### Interactive CLI

Done. `cairn fix` is interactive by default: each repair is presented with a before/after preview and confidence, and you accept or skip it. `--yes` runs non-interactively for CI and scripting, and piped stdin falls back to applying only high-confidence fixes.

### Also planned

- Format-preserving YAML rewrites (comments, ordering, whitespace)
- SARIF output for CI and security tooling integrations
- CRD schema support via `--schema-location`
- Broader resource coverage (Service, Ingress, NetworkPolicy)
- Signed-image and supply-chain policy rules (cosign / sigstore)
- Expanded Pod Security Standards coverage

---

## Known limitations, TODOs, and audit findings

This section tracks what we know is rough. It's here on purpose — Cairn will be open source, and we'd rather document gaps than hide them.

See [SECURITY.md](SECURITY.md) for vulnerability reporting.

### Bugs to fix before public release

**Critical / High**

- **DocIndex collision on directory scans** — [`internal/parser/parser.go`](internal/parser/parser.go) resets document index per file, but [`internal/fixer/fixer.go`](internal/fixer/fixer.go) keys working documents by index only. Scanning a directory can cause fixes to target the wrong manifest across files.
- **YAML round-trip fidelity** — the fixer re-marshals with `yaml.Marshal`, which drops comments, field ordering, anchors, and formatting. Even a one-field fix rewrites the whole file.
- **In-place writes without atomic rename or backup** — `fix` overwrites the source file directly via `os.WriteFile`. A crash mid-write can corrupt manifests.
- **`--out` path traversal** — [`outputPath`](internal/fixer/fixer.go) joins `outDir` with the source path without containment checks. A relative source like `../../outside.yaml` can escape the output directory.
- **Unbounded file and stdin reads** — no size or depth limits on YAML input (YAML bomb / DoS risk).
- **`compat` checks stale findings post-fix** — [`cmd/compat.go`](cmd/compat.go) evaluates success against the original scan, not a re-scan after applying fixes.
- **Stdin fix flow broken** — stdin is consumed on the first scan; the post-fix verification re-scan reads nothing. Fixes on `-` without `--out` are never written.

### Security and safety

**Medium**

- In-place overwrite should require an explicit `--in-place` flag rather than being the silent default.
- Cluster mode loads kubeconfig credentials; minimum RBAC scope and behavior are not documented.
- `--cluster` help text mentions PSS context, but the probe only appends a server version string to two rule messages today.
- Output files are written with world-readable permissions (`0o644`).
- Multi-doc splitting uses naive `---` delimiter matching, which can break if `---` appears inside multiline strings.

### Engineering and OSS hygiene

**Medium / Low**

- No `CONTRIBUTING.md` or `CODE_OF_CONDUCT.md` yet.
- CI runs `go test` and `go build` only — no lint (`golangci-lint`), race detector, or coverage reporting.
- `internal/fixer`, `internal/compat`, `internal/schema`, `internal/cluster`, and `internal/report` have no tests.
- The built `cairn` binary is not in [`.gitignore`](.gitignore).
- LICENSE copyright ("Michael Ott") does not match the module owner (`github.com/jackalgg/cairn`).
- No version subcommand or release workflow (GoReleaser, Homebrew, etc.).
- No `examples/` directory beyond [`test.yaml`](test.yaml).

### Technical debt

**Medium / Low**

- API migration in [`internal/compat/compat.go`](internal/compat/compat.go) only updates `apiVersion`/`kind`. Ingress migration from `extensions/v1beta1` needs field-level changes (`pathType`, `ingressClassName`, etc.).
- `targetVersion` appears in compat messages but does not gate which migrations run.
- `--from-error` and dry-run verification map `schema-unknown-field` (now wired to a remove-field fix via [`internal/repair/schemafix`](internal/repair/schemafix)) and `api-version-deprecated`; coverage of other admission errors is still partial.
- Finding paths use `containers.<name>` notation, but containers in YAML are a list, not a map keyed by name.
- Dead code in [`internal/policy/helpers.go`](internal/policy/helpers.go) (`ensureNestedMap` is unused and contains a bug).
- Fix orchestration is duplicated between [`cmd/fix.go`](cmd/fix.go) and [`cmd/compat.go`](cmd/compat.go).

### Policy coverage gaps

- Partial Pod Security Standards: missing checks for `privileged`, `allowPrivilegeEscalation`, capabilities, seccomp, and `runAsUser`.
- No checks on `initContainers` or `ephemeralContainers`.
- `image-floating-tag` warns on `:latest` and untagged images but cannot pin without registry lookup.
- No signed-image or supply-chain rules (cosign, sigstore, SBOM) despite being part of the project vision.

---

## License

Apache License 2.0 — see [LICENSE](LICENSE).
