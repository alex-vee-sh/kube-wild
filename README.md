kubectl-wild
============

Wildcard-friendly wrapper for common kubectl commands (get, describe, delete). Installed as a kubectl plugin `kubectl-wild` and invoked as `kubectl wild ...`.

Why
---

Shell globs don't apply to Kubernetes resource names. This plugin lets you use simple wildcard patterns like `*` and `?` to match resource names, then delegates to `kubectl` for output and actions.

Install (via Krew)
------------------

Prereqs:

- `kubectl krew` is installed. See `https://krew.sigs.k8s.io/docs/user-guide/setup/install/`.
- Ensure Krew is on PATH (add this to your shell profile):

```bash
export PATH="${KREW_ROOT:-$HOME/.krew}/bin:$PATH"
```

Install the plugin (once it’s published to the index):

```bash
kubectl krew install wild
kubectl wild --help
```

Install (local/dev)
-------------------

Build:

```bash
GO111MODULE=on go build -o ./kubectl-wild .
```

Put the binary on your PATH (so `kubectl` discovers `kubectl-wild` as `kubectl wild`):

```bash
mv ./kubectl-wild /usr/local/bin/
```

Confirm installation:

```bash
kubectl wild --help
```

Usage
-----

`kubectl wild (get|delete|describe) [resource] [pattern] [flags...] [-- extra]`

Always quote your patterns to prevent your shell from expanding them.

Key flags:

- Matching: `--regex` | `--contains` | `--fuzzy` (`--fuzzy-distance N`) | `--prefix/-p VAL` | `--match VAL` | `--exclude VAL` | `--ignore-case`
- Scope: `-n/--namespace NS` | `-A/--all-namespaces` | `--ns NS` | `--ns-prefix PFX` | `--ns-regex RE`
- Safety: `--dry-run` | `--server-dry-run` | `--confirm-threshold N` | `--yes/-y` | `--preview [list|table]` | `--no-color`
- Pod filters: `--older-than DURATION` | `--younger-than DURATION` | `--pod-status STATUS` | `--unhealthy`
- Label filters: `--label key=glob` | `--label-prefix key=prefix` | `--label-contains key=sub` | `--label-regex key=regex` | `--label-key-regex regex`
- Grouping: `--group-by-label key` (adds `-L key` to kubectl table) | `--colorize-labels`
- Node/container filters: `--node NAME` | `--node-prefix PFX` | `--node-regex RE` | `--restarts EXPR` (`>N`, `>=N`, `<N`, `<=N`, `=N`) | `--containers-not-ready` | `--reason REASON` | `--container-name NAME`
- Output: `--output json`

Examples:

```bash
# Defaults: resource pods, pattern * in current namespace
kubectl wild get

# Glob match in a namespace
kubectl wild get pods 'a*' -n default

# Regex across all namespaces (single kubectl table)
kubectl wild get pods --regex '^(api|web)-' -A

# Contains (provide pattern via --match)
kubectl wild get pods --contains --match pi- -n dev-x

# Prefix helpers
kubectl wild get pods --prefix foo -n default   # equivalent to 'foo*'
kubectl wild get pods -p foo -n default        # short for --prefix

# Fuzzy (handles hashed pod names)
kubectl wild get pods --fuzzy --fuzzy-distance 1 --match apu-1 -n dev-x

# Namespace filters
kubectl wild get pods -A --ns-prefix prod-

# Pod age/status filters
kubectl wild get pods -A --younger-than 10m --pod-status Running
kubectl wild get pods -A --older-than 1h --pod-status Pending

# Unhealthy pods (not clean Running, not Succeeded)
kubectl wild get pods -A --unhealthy

# Label filters and grouping
kubectl wild get pods -A --label 'app=web-*' --group-by-label app
kubectl wild get pods -A --label-prefix 'app=web' --group-by-label app
kubectl wild get pods -A --label-key-regex '^app$' --group-by-label app
# Add a colored summary above the table (optional)
kubectl wild get pods -A --label 'app=*' --group-by-label app --colorize-labels

# Node and container health filters
kubectl wild get pods -A --node-prefix worker-
kubectl wild get pods -A --restarts '>0'
kubectl wild get pods -A --containers-not-ready
kubectl wild get pods -A --reason CrashLoopBackOff
kubectl wild get pods -A --reason OOMKilled --container-name app
```

- Flags after the pattern are passed through to `kubectl` (e.g., `-n`, `-A`, `-l`).
- For `get`, output is rendered as a single kubectl table; with `-A` the NAMESPACE column is included, like kubectl.
- For `describe`, the plugin runs `kubectl describe` on the matched set.
- For `delete`, the plugin previews matches and always asks for confirmation (`y/N`). The prompt is bright red by default to prevent accidents.

Delete preview and colors
-------------------------

- Default preview is a red column list of targets.
- With `-A`, default preview switches to a kubectl-style table (or pass `--preview table` explicitly).
- Disable color with `--no-color`.

Examples:

```bash
# Column preview (red)
kubectl wild delete pods -p te -n default

# Table preview with NAMESPACE column
kubectl wild delete pods -p te -A --preview table
```

Namespace filters and safety
----------------------------

- Filter namespaces (applied after discovery):
  - `--ns <ns>`: include only exact namespaces (repeatable)
  - `--ns-prefix <prefix>`: include namespaces by prefix (repeatable)
  - `--ns-regex <re>`: include namespaces by regex (repeatable)
- Safety:
  - `--server-dry-run`: perform delete with `--dry-run=server`
  - `--confirm-threshold N`: block delete if matches > N unless `-y`

Examples
--------

```bash
# List pods that start with "api-" in kube-system
kubectl wild get pods 'api-*' -n kube-system

# Delete all test* pods in the current namespace (with prompt)
kubectl wild delete pods 'test*'
```

Notes
-----

- Supported wildcards: `*` (any sequence), `?` (single char). Matching is case-sensitive.
- Place flags after the pattern; flags before the pattern are not currently parsed.
- The plugin shells out to `kubectl` and therefore respects your current context, kubeconfig, RBAC, etc.
- Logs are intentionally not supported; prefer `stern` for logs use-cases.

Disclaimer
----------

Use at your own risk. This tool expands patterns and then shells out to `kubectl` to perform actions. Always review the matched resource list before destructive operations. The `delete` verb will prompt for confirmation by default; use `--dry-run` to preview and `-y/--yes` to skip the prompt when you’re certain.

Krew packaging (template)
-------------------------

A manifest template is provided at `krew/wild.yaml`. To publish:

1. Fork this repo to GitHub and set proper module path in `go.mod`.
2. Build release archives per-platform named like `kubectl-wild_{os}_{arch}.tar.gz` containing a single `kubectl-wild` binary.
3. Update `krew/wild.yaml` `version`, `sha256`, and `uri` entries for each platform.
4. Test install locally:

```bash
kubectl krew install --manifest=./krew/wild.yaml --archive=./dist/kubectl-wild_darwin_amd64.tar.gz
```

5. Submit to the krew-index following their guidelines.

License
-------

MIT


