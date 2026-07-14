# Cairn

**Cairn repairs mechanically broken YAML manifests** — the kind that makes a
Kubernetes or Docker manifest die in the terminal with `mapping values are not
allowed here` or `error validating data`. It reconstructs the document's real
structure and repairs indentation, missing list markers, jammed `key:value`
colons, and wrong or missing `apiVersion`/`kind`, so the file parses and applies
again.

Every repair runs as a small, isolated pass with a declared *edit budget* —
the only kind of change it is allowed to make — and each pass's output is
verified against that budget before it is accepted. Your values, quoting,
comments, and block scalars are left exactly as they were.

```console
$ cairn fix --dry-run deploy.yaml
--- deploy.yaml
+++ deploy.yaml (fixed)
  spec:
-    containers:
+  containers:
    - image: busybox
      name: app
-    dnsPolicy: ClusterFirst
+  dnsPolicy: ClusterFirst
```

---

## Why it's different

Most "fix my YAML" attempts guess a patch for one error, re-parse, and repeat —
which falls apart the moment a file has several indentation mistakes at once,
because the errors interact.

Cairn doesn't guess or iterate. It **rebuilds the entire nesting tree in a single
pass** from signals that aren't the (corrupt) indentation:

- **Structure** — a bare `key:` opens a block; `- ` opens a list item.
- **Schema** — for Kubernetes manifests, a field name tells you its parent. So a
  `dnsPolicy` that got buried inside a container is pulled back out to the
  `PodSpec` where it belongs, no matter how far the broken file had indented it.

Then it re-emits the document with clean two-space indentation. Because it's a
reconstruction rather than a search, **fixing ten simultaneous indentation errors
is no harder than fixing one.**

---

## Install

Requires Go 1.26+.

```bash
git clone https://github.com/jackalgg/cairn.git
cd cairn
go build -o cairn .
```

---

## Usage

```bash
# Preview the fix as a diff (writes nothing)
cairn fix --dry-run broken.yaml

# Print the repaired YAML to stdout (default — still writes nothing)
cairn fix broken.yaml

# Overwrite the file in place
cairn fix --in-place broken.yaml

# CI / pre-commit: exit non-zero if any file needs repair, change nothing
cairn fix --check ./manifests/

# Works on a file, a directory of .yaml/.yml files, or stdin
cat broken.yaml | cairn fix -
```

`path` can be a file, a directory (all `.yaml`/`.yml` files), or `-` for stdin.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show a before/after diff instead of writing |
| `--in-place` | Overwrite the source file(s) |
| `--check` | Report whether files need repair; make no changes; exit non-zero if any do |

`fix` exits non-zero if a file still doesn't parse after reindenting — that means
it's broken in a way Cairn doesn't fix (see [Scope](#scope)).

---

## What it fixes

Any number of these, simultaneously:

- **Tabs** used for indentation (converted to spaces).
- **Wrong depth** — lines indented too far or not far enough.
- **Collapsed blocks** — a mapping's children flattened to column 0.
- **Odd offsets** — the classic "one extra space" that breaks sibling alignment.
- **Fields in the wrong block** — for known Kubernetes kinds, a field indented
  into the wrong parent is moved back where the schema says it belongs.
- **Colon spacing** — `image:nginx` → `image: nginx`, and `name:   app` →
  `name: app`. Schema-aware, so a scalar list item like `- kill:9` under
  `command:` is *not* mistaken for a mapping.
- **Missing list markers** — a `containers:` item that lost its `- ` (detected
  by schema), a second item merged into the first (detected by a repeated key),
  or a scalar `args:`/`command:` entry that lost its dash.
- **Wrong or missing identity** — `apiVersion: extensions/v1beta1` on a
  Deployment becomes `apps/v1`; a missing `apiVersion` is inserted (it is
  derivable from the kind); `kind: deployment` becomes `Deployment`, which also
  unlocks the schema-aware repairs above. Every such repair is reported on
  stderr as `file:line: apiVersion "extensions/v1beta1" → "apps/v1"`.

Kubernetes kinds understood today: Pod, Deployment (and ReplicaSet / DaemonSet /
StatefulSet), Job, CronJob, Service, ConfigMap / Secret, and their nested types
(PodSpec, Container, ObjectMeta, probes, security contexts, volumes, ports, env…).
Anything else — including plain YAML like CI configs and compose files — is
repaired structurally (schema-free), which still fixes tabs, depth, collapsed
blocks, and odd offsets.

---

## Scope

Cairn repairs what is **mechanically derivable** — where the file itself (plus
the Kubernetes object model) proves what the author meant. It will not:

- correct misspelled field names *(planned next — high-confidence only)*,
- guess at values, delete unknown fields, or reorder anything,
- validate your manifest against a full schema, lint for security, or talk to a
  cluster.

If a file is broken in a way Cairn can't prove a fix for, it repairs what it
can and tells you the file still doesn't parse, rather than silently guessing.
There are excellent dedicated tools for validation and policy (kubeconform,
kube-score, checkov) — Cairn deliberately stays out of their lane.

**Safety:** every pass has a declared edit budget — reindent may only change
leading whitespace, the marker pass may only insert `- `, colon repair may only
adjust spacing, identity repair may only rewrite/insert `apiVersion`/`kind`
lines. After each pass an independent verifier re-checks the output against
that budget; an out-of-contract edit is discarded and reported instead of
written. A bug in any single pass cannot corrupt your file.

---

## How it works

```
internal/reindent/
  pipeline.go   the verified repair chain: identity → colons → markers → reindent
  identity.go   apiVersion/kind repair (table-driven, per document)
  colon.go      colon-spacing repair (schema-aware, skips scalar seq items)
  markers.go    missing list-marker insertion (schema-confirmed sequences only)
  reindent.go   Reindent([]byte) → rebuild the scope tree, re-emit at canonical depth
  schema.go     compact Kubernetes type table (field → parent) shared by every pass
cmd/            thin CLI: read → Fix pipeline → validate → print / diff / write
internal/parser generic YAML validation (ValidateYAML, ParseErrorLine)
```

The core is `placeKey`: for each line, pop the scope stack until the frame that
should parent the key is on top. A field declared by a specific ancestor type
(schema) beats free-form absorption, which is what recovers a field over-indented
into a `labels:` block. Unknown documents degrade gracefully to a pure
relative-indent renormalizer through the same code path.

```bash
go test ./...
```

---

## Roadmap

Cairn is focused on **core user utility**: making broken manifests parse and
apply, without turning into a linter or policy engine. Planned work:

- **Field-typo repair** — a unique, unambiguous match against the schema fields
  of the current scope (`contaieners:` → `containers:`) is fixed and reported;
  anything fuzzier becomes a suggestion, never an edit.
- **Clearer "can't fix" messages** — name the line and error class instead of the
  raw parser error.
- **Wider schema coverage** — more kinds (Ingress, NetworkPolicy, PVC, HPA, RBAC)
  and Docker Compose, so every schema-aware pass applies to more manifests.
- **Adoption** — pre-commit hook and GitHub Action (using `--check`).

Explicitly **out of scope** (use a dedicated tool): security/policy hardening,
full schema validation, live-cluster interaction, AI repair, deleting or
reordering anything you wrote.

---

## Contributing

Cairn is open source and early. The highest-leverage contributions right now are
real-world broken manifests that Cairn gets wrong — add them as fixtures under
[`testdata/repair/`](testdata/repair) with a test in
[`internal/reindent/reindent_test.go`](internal/reindent/reindent_test.go).
Expanding the schema table in [`schema.go`](internal/reindent/schema.go) is pure
data and a great first PR.

See [SECURITY.md](SECURITY.md) for vulnerability reporting.

## License

Apache License 2.0 — see [LICENSE](LICENSE).
