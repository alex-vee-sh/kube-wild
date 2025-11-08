## Unreleased

- (no changes yet)

# Changelog

## v1.0.9
- Performance optimizations:
  - Pre-compile regex patterns for include/exclude filters when using `--regex` mode
  - Pre-compile regex patterns for label and annotation regex filters (`--label-regex`, `--annotation-regex`)
  - Pre-compile node regex filters (`--node-regex`) for faster node filtering
  - All regex patterns compiled once per query instead of per-resource check
- Stability improvements:
  - Improved error handling for time parsing in resource discovery (invalid timestamps handled gracefully)
- Test coverage improvements:
  - Added comprehensive tests for `labelValueMatches` (all label modes: glob, prefix, contains, regex)
  - Added tests for `containsFlag` and `containsFlagWithPrefix` functions
  - Added tests for `runVerbPassthrough` optimization path
  - Added tests for `matchSingle` function (all match modes)
  - Overall test coverage increased from 60.8% to 63.1%

## v1.0.8
- New `top` verb: `kubectl wild top pods|nodes` for resource usage metrics (CPU/memory)
  - Supports pods and nodes resources only
  - OpenShift compatibility: automatically uses `oc adm top` when `WILD_KUBECTL=oc` is set
  - Handles `kubectl top` limitations: multiple pod matches show all pods in namespace
- Annotation filtering: mirror of label filtering functionality
  - `--annotation key=glob`, `--annotation-prefix key=prefix`, `--annotation-contains key=sub`, `--annotation-regex key=regex`
  - `--annotation-key-regex regex` for filtering by annotation key presence
  - AND logic across different keys, OR logic across multiple filters of the same key
- Performance optimizations:
  - Regex pre-compilation: regex patterns compiled once per query instead of per-resource check
  - Lazy nsname computation: namespace/name string only computed when needed
  - Optimized Matches call order: check name match first, avoid unnecessary nsname checks

## v1.0.7
- Removed hardcoded resource normalization; fully dynamic resolution via `kubectl api-resources`
- Passthrough optimization: resources already in canonical form (with dots) skip discovery when no filtering needed
- Support `WILD_KUBECTL`/`KUBECTL` environment variables to use `oc` instead of `kubectl` (OpenShift compatibility)
- Resource resolution: core resource shortnames stay as-is (e.g., `svc` stays `svc`); only CRDs resolve to canonical form
- Discovery-first approach: try discovery with resource as-is, only resolve if discovery fails (for CRDs)
- Performance improvement for canonical resources (e.g., `clusteroperators.config.openshift.io`) when no filtering needed

## v1.0.6
- Accept equals form for namespace: `-n=NS`, `--namespace=NS` with wildcard detection
- Debug logs now show `AllNamespaces`, `Namespace`, and `DiscoveryFlags` to aid troubleshooting

## v1.0.5
- Ensure namespace wildcard via `-n "pattern"` is included in released binaries:
  - `-n` with globs implies `-A` for discovery and is translated to prefix/regex filters
  - Keeps single-table `get` output with the NAMESPACE column
- Documentation refined for `-n` wildcard behavior; no functional changes beyond packaging

## v1.0.4
- Namespace wildcard filtering via `-n "prod-*"` (implicitly uses `-A` for discovery), works across all resources.
- Dynamic CRD support: resources are canonicalized against `kubectl api-resources`, so singular/shortname/group-qualified forms resolve (e.g., `bgppeer`, `bgpp`, `bgppeers.metallb.io`).
- Respect cluster-scoped kinds: suppress `-A`/`-n` and handle get/describe/delete without per-namespace logic.
- Added tests validating `-A` single-table output for services/routes, wildcard `-n` behavior, cluster-scoped handling, and CRD canonicalization.
- README updated with examples and behavior notes.
## v1.0.3
- Labels:
  - New selectors: `--label key=glob`, `--label-prefix key=pfx`, `--label-contains key=sub`, `--label-regex key=re`
  - Label key presence: `--label-key-regex re` (AND across repeats)
  - Grouping UX: `--group-by-label key` (adds `-L key` to kubectl table); optional `--colorize-labels` summary
- Node filters: `--node`, `--node-prefix`, `--node-regex`
- Pod health:
  - Restarts filter: `--restarts >N|>=N|<N|<=N|=N`
  - `--containers-not-ready` matches pods with any not-ready containers
  - Reason filtering: `--reason Reason` with optional `--container-name name`
- Discovery enriched: nodeName, restart counts, ready flags, ownerReferences, reasons per-container
- CI: release artifacts embed version metadata via ldflags (`version`, `commit`, `date`)
- Tests: added coverage for label filters, node prefix, restart expressions, container-scoped reasons, label-key regex

## v1.0.2
- Pod status filtering tightened:
  - `--pod-status Running` now excludes pods with error reasons (e.g., CrashLoopBackOff/Error)
  - Added `--unhealthy` (alias: `-unhealthy`) to show non-healthy pods (not clean Running and not Succeeded)
- Fuzzy matching refined to avoid inner-substring overmatches (e.g., `ngin` no longer matches `pending-forever`)
- Fixed batching infinite loop when batch size was zero (tests could hang)
- More deterministic tests; expanded coverage for fuzzy/age/status/namespace filters
- Manual test scenarios validated; single-table `get -A` preserved

## v0.0.3
- Namespace filters: `--ns`, `--ns-prefix`, `--ns-regex`
- Safety: `--confirm-threshold`, `--server-dry-run`
- Previews: default table on -A; red list shows NAMESPACE	RESOURCE/NAME
- Docs update

## v0.0.2
- Prefix matching flag `--prefix` and `-p`
- Delete preview improvements:
  - Red column preview; resource/name with namespace
  - Table preview (`--preview table`) and default table when using `-A`
  - Bright red confirmation prompt
- Get with `-A` shows a single kubectl table with NAMESPACE column
- README, LICENSE, Krew manifest updates

## v0.0.1
- Initial release: wildcard matching (glob/regex/contains), include/exclude, ignore-case
- Verbs: get, describe, delete (with confirmation, `--dry-run`, `-y`)
- All-namespaces handling and batching to avoid long arg lists
- Makefile, tests, Krew manifest template, GitHub Actions release workflow
