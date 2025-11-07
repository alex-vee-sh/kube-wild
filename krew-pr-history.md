# Krew PR History and Templates

## v1.0.8 (kubectl-wild)

- Summary:
  - New `top` verb for resource usage metrics (pods/nodes)
  - Annotation filtering (mirrors label filtering)
  - Performance optimizations: regex pre-compilation, lazy nsname computation

- PR body template:

```
Title: Update plugin: wild (v1.0.8)

This updates `wild` – a kubectl plugin for wildcard-friendly operations (get/describe/delete/top).

Highlights:
- Glob, regex, contains, prefix, and fuzzy matching (handles hashed names)
- Namespace filters: --ns/--ns-prefix/--ns-regex, wildcard `-n "prod-*"`, and equals form `-n=prod-*`
- Labels: value globs/prefix/contains/regex; key presence via --label-key-regex
- Annotations: value globs/prefix/contains/regex; key presence via --annotation-key-regex
- Group-by label: --group-by-label <key> (adds -L column), optional --colorize-labels summary
- Nodes: --node/--node-prefix/--node-regex
- Pod health: --restarts (>N, >=N, <N, <=N, =N), --containers-not-ready
- Reasons: --reason OOMKilled|CrashLoopBackOff (optionally --container-name <name>)
- Resource usage: `top` verb for pods/nodes (CPU/memory metrics)
- Safe deletes: bright red previews, --dry-run/--server-dry-run, --confirm-threshold, -y
- Native output: single kubectl table with NAMESPACE column for -A
- Dynamic CRD canonicalization and cluster-scope handling
- OpenShift support via WILD_KUBECTL=oc (including `oc adm top` for top verb)
- Performance optimizations: regex pre-compilation, lazy string operations

Changelog v1.0.8:
- New `top` verb: `kubectl wild top pods|nodes` for resource usage metrics
- Annotation filtering: --annotation, --annotation-prefix, --annotation-contains, --annotation-regex, --annotation-key-regex
- Performance: regex patterns pre-compiled once per query instead of per-resource
- Performance: lazy nsname computation (only when needed)
- Performance: optimized Matches call order to avoid unnecessary checks
- OpenShift: `top` verb automatically uses `oc adm top` when WILD_KUBECTL=oc

Manifest:
- `krew/wild.yaml` generated via CI with SHA256 for darwin/linux/windows (amd64/arm64)
- URIs point to GitHub release assets for v1.0.8

Checklist:
- [x] `kubectl krew` validate (locally)
- [x] SHA256s computed
- [x] Tested on macOS and Linux
- [x] Tested with OpenShift (oc adm top)
```

---

Note: When opening the PR, attach the `krew/wild.yaml` from the v1.0.8 release workflow output.

## v1.0.7 (kubectl-wild)

- Summary:
  - Fully dynamic resource resolution (no hardcoding); try discovery first, resolve only if needed
  - Passthrough optimization for canonical resources (with dots) when no filtering needed
  - Support `WILD_KUBECTL`/`KUBECTL` env vars to use `oc` instead of `kubectl` (OpenShift)
  - Core shortnames stay as-is (`svc` → `svc`); only CRDs resolve to canonical form
  - Performance improvement for canonical resources

- PR body template:

```
Title: Update plugin: wild (v1.0.7)

This updates `wild` – a kubectl plugin for wildcard-friendly operations (get/describe/delete).

Highlights:
- Glob, regex, contains, prefix, and fuzzy matching (handles hashed names)
- Namespace filters: --ns/--ns-prefix/--ns-regex, wildcard `-n "prod-*"`, and equals form `-n=prod-*`
- Labels: value globs/prefix/contains/regex; key presence via --label-key-regex
- Group-by label: --group-by-label <key> (adds -L column), optional --colorize-labels summary
- Nodes: --node/--node-prefix/--node-regex
- Pod health: --restarts (>N, >=N, <N, <=N, =N), --containers-not-ready
- Reasons: --reason OOMKilled|CrashLoopBackOff (optionally --container-name <name>)
- Safe deletes: bright red previews, --dry-run/--server-dry-run, --confirm-threshold, -y
- Native output: single kubectl table with NAMESPACE column for -A
- Dynamic CRD canonicalization and cluster-scope handling
- OpenShift support via WILD_KUBECTL=oc

Changelog v1.0.7:
- Removed hardcoded resource normalization; fully dynamic via kubectl api-resources
- Passthrough optimization for canonical resources (with dots) when no filtering needed
- Support WILD_KUBECTL/KUBECTL env vars to use oc instead of kubectl
- Core shortnames stay as-is (svc → svc); only CRDs resolve to canonical
- Discovery-first: try as-is, resolve only if discovery fails
- Performance improvement for canonical resources

Manifest:
- `krew/wild.yaml` generated via CI with SHA256 for darwin/linux/windows (amd64/arm64)
- URIs point to GitHub release assets for v1.0.7

Checklist:
- [x] `kubectl krew` validate (locally)
- [x] SHA256s computed
- [x] Tested on macOS and Linux
```

---

Note: When opening the PR, attach the `krew/wild.yaml` from the v1.0.7 release workflow output.

## v1.0.6 (kubectl-wild)

- Summary:
  - Support equals-form namespace flags: `-n=NS`, `--namespace=NS` (wildcard-aware)
  - Debug output includes `AllNamespaces`, `Namespace`, and `DiscoveryFlags`

- PR body template:

```
Title: Update plugin: wild (v1.0.6)

This updates `wild` – a kubectl plugin for wildcard-friendly operations (get/describe/delete).

Highlights:
- Glob, regex, contains, prefix, and fuzzy matching (handles hashed names)
- Namespace filters: --ns/--ns-prefix/--ns-regex, wildcard `-n "prod-*"`, and equals form `-n=prod-*`
- Labels: value globs/prefix/contains/regex; key presence via --label-key-regex
- Group-by label: --group-by-label <key> (adds -L column), optional --colorize-labels summary
- Nodes: --node/--node-prefix/--node-regex
- Pod health: --restarts (>N, >=N, <N, <=N, =N), --containers-not-ready
- Reasons: --reason OOMKilled|CrashLoopBackOff (optionally --container-name <name>)
- Safe deletes: bright red previews, --dry-run/--server-dry-run, --confirm-threshold, -y
- Native output: single kubectl table with NAMESPACE column for -A
- Dynamic CRD canonicalization and cluster-scope handling

Changelog v1.0.6:
- Accept `-n=NS` / `--namespace=NS` with wildcard detection (implies -A on discovery)
- Improve debug output to include discovery-related flags

Manifest:
- `krew/wild.yaml` generated via CI with SHA256 for darwin/linux/windows (amd64/arm64)
- URIs point to GitHub release assets for v1.0.6

Checklist:
- [x] `kubectl krew` validate (locally)
- [x] SHA256s computed
- [x] Tested on macOS and Linux
```

---

Note: When opening the PR, attach the `krew/wild.yaml` from the v1.0.6 release workflow output.

## v1.0.5 (kubectl-wild)

- Summary:
  - Ship namespace wildcard via `-n "pattern"` (implies `-A`, translates to prefix/regex)
  - Preserve single-table `get -A` output with NAMESPACE column
  - Docs clarify `-n` wildcard usage

- PR body template:

```
Title: Update plugin: wild (v1.0.5)

This updates `wild` – a kubectl plugin for wildcard-friendly operations (get/describe/delete).

Highlights:
- Glob, regex, contains, prefix, and fuzzy matching (handles hashed names)
- Namespace filters: --ns/--ns-prefix/--ns-regex, and wildcard `-n "prod-*"` across namespaces
- Labels: value globs/prefix/contains/regex; key presence via --label-key-regex
- Group-by label: --group-by-label <key> (adds -L column), optional --colorize-labels summary
- Nodes: --node/--node-prefix/--node-regex
- Pod health: --restarts (>N, >=N, <N, <=N, =N), --containers-not-ready
- Reasons: --reason OOMKilled|CrashLoopBackOff (optionally --container-name <name>)
- Safe deletes: bright red previews, --dry-run/--server-dry-run, --confirm-threshold, -y
- Native output: single kubectl table with NAMESPACE column for -A
- Dynamic CRD canonicalization and cluster-scope handling

Changelog v1.0.5:
- Ensure released binaries include `-n` wildcard behavior (implies -A, prefix/regex translation)
- Keep single-table output for `get -A`
- Documentation clarifications

Manifest:
- `krew/wild.yaml` generated via CI with SHA256 for darwin/linux/windows (amd64/arm64)
- URIs point to GitHub release assets for v1.0.5

Checklist:
- [x] `kubectl krew` validate (locally)
- [x] SHA256s computed
- [x] Tested on macOS and Linux
```

---

Note: When opening the PR, attach the `krew/wild.yaml` from the v1.0.5 release workflow output.

## v1.0.4 (kubectl-wild)

- Summary:
  - Namespace wildcard via `-n "prod-*"` (implies `-A` for discovery)
  - Dynamic CRD canonicalization against `kubectl api-resources` (singular/shortname/group-qualified)
  - Respect cluster-scoped kinds (suppress `-A`/`-n`; handle get/describe/delete accordingly)
  - Tests for single-table `-A` output (services/routes), wildcard `-n`, cluster-scoped behavior, CRD resolution
  - README updated with examples and notes

- PR body template:

```
Title: Update plugin: wild (v1.0.4)

This updates `wild` – a kubectl plugin for wildcard-friendly operations (get/describe/delete).

Highlights:
- Glob, regex, contains, prefix, and fuzzy matching (handles hashed names)
- Namespace filters: --ns/--ns-prefix/--ns-regex, and wildcard `-n "prod-*"` across namespaces
- Labels: value globs/prefix/contains/regex; key presence via --label-key-regex
- Group-by label: --group-by-label <key> (adds -L column), optional --colorize-labels summary
- Nodes: --node/--node-prefix/--node-regex
- Pod health: --restarts (>N, >=N, <N, <=N, =N), --containers-not-ready
- Reasons: --reason OOMKilled|CrashLoopBackOff (optionally --container-name <name>)
- Safe deletes: bright red previews, --dry-run/--server-dry-run, --confirm-threshold, -y
- Native output: single kubectl table with NAMESPACE column for -A
- Dynamic CRD canonicalization and cluster-scope handling

Changelog v1.0.4:
- Namespace wildcard via `-n "prod-*"` (adds -A for discovery)
- Resolve resources dynamically against `kubectl api-resources` (singular/short/qualified)
- Respect cluster-scoped kinds (no -A/-n forwarding)
- Added tests for single-table -A (services/routes), wildcard -n, cluster-scope, CRD canonicalization
- README updates

Manifest:
- `krew/wild.yaml` generated via CI with SHA256 for darwin/linux/windows (amd64/arm64)
- URIs point to GitHub release assets for v1.0.4

Checklist:
- [x] `kubectl krew` validate (locally)
- [x] SHA256s computed
- [x] Tested on macOS and Linux
```

---

Note: When opening the PR, attach the `krew/wild.yaml` from the v1.0.4 release workflow output.

## v1.0.3 (kubectl-wild)

- Summary:
  - Label selectors: `--label`, `--label-prefix`, `--label-contains`, `--label-regex`
  - Label key presence: `--label-key-regex`
  - Grouping/UX: `--group-by-label <key>` (adds `-L <key>` to kubectl table), optional `--colorize-labels`
  - Node filters: `--node`, `--node-prefix`, `--node-regex`
  - Pod health filters: `--restarts >N`, `--containers-not-ready`, `--reason <Reason>` with `--container-name <name>`
  - CI release embeds version metadata via ldflags: version, commit, UTC date

- PR body template:

```
Title: New plugin: wild (v1.0.3)

This updates `wild` – a kubectl plugin for wildcard-friendly operations (get/describe/delete).

Highlights:
- Glob, regex, contains, prefix, and fuzzy matching (handles hashed names)
- Namespace filters: --ns/--ns-prefix/--ns-regex
- Labels: value globs/prefix/contains/regex; key presence via --label-key-regex
- Group-by label: --group-by-label <key> (adds -L column), optional --colorize-labels summary
- Nodes: --node/--node-prefix/--node-regex
- Pod health: --restarts (>N, >=N, <N, <=N, =N), --containers-not-ready
- Reasons: --reason OOMKilled|CrashLoopBackOff (optionally --container-name <name>)
- Safe deletes: bright red previews, --dry-run/--server-dry-run, --confirm-threshold, -y
- Native output: single kubectl table with NAMESPACE column for -A

Changelog v1.0.3:
- Added label selectors and label-key-regex presence filtering
- Added node filters and pod health filters (restarts, not-ready)
- Added reason filtering, container-scoped
- Group-by label (-L column) with optional colored summary
- Release binaries embed version metadata (tag, short SHA, UTC date)

Manifest:
- `krew/wild.yaml` generated via CI with SHA256 for darwin/linux/windows (amd64/arm64)
- URIs point to GitHub release assets for v1.0.3

Checklist:
- [x] `kubectl krew` validate (locally)
- [x] SHA256s computed
- [x] Tested on macOS and Linux
```

---

Note: When opening the PR, attach the `krew/wild.yaml` from the v1.0.3 release workflow output.

## v1.0.2 (kubectl-wild)

- Summary:
  - Tighten `--pod-status Running` (exclude CrashLoopBackOff/Error)
  - Add `--unhealthy` flag (show non-healthy pods)
  - Improve fuzzy matching; avoid inner-substring overmatches
  - Fix batching when batch size is zero; expand tests

- PR body template:

```
Title: New plugin: wild (v1.0.2)

This adds/updates `wild` – a kubectl plugin for wildcard-friendly operations (get/describe/delete).

Highlights:
- Glob, regex, contains, prefix, and fuzzy matching (handles hashed names)
- Namespace filters: --ns/--ns-prefix/--ns-regex
- Pod filters: --pod-status, --older-than/--younger-than, and new --unhealthy
- Safe deletes: bright red previews, --dry-run/--server-dry-run, --confirm-threshold, -y
- Native output: single kubectl table with NAMESPACE column for -A

Changelog v1.0.2:
- `--pod-status Running` excludes pods with CrashLoopBackOff/Error
- Add `--unhealthy` (non-healthy pods)
- Refined fuzzy matching; reduced false positives
- Fixed batching loop when batch size was 0; more tests

Manifest:
- `krew/wild.yaml` generated via CI with SHA256 for darwin/linux/windows (amd64/arm64)
- URIs point to GitHub release assets for v1.0.2

Checklist:
- [x] `kubectl krew` validate (locally)
- [x] SHA256s computed
- [x] Tested on macOS and Linux
```

---

Note: When opening the PR, attach the `krew/wild.yaml` from the v1.0.2 release workflow output.
