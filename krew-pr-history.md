# Krew PR History and Templates

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
