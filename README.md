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

Always quote your patterns to prevent your shell from expanding them:

```bash
# Defaults: resource pods, pattern * in current namespace
kubectl wild get

# Wildcards
kubectl wild get pods 'a*' -n default
kubectl wild describe pods --regex '^(api|web)-' -A
kubectl wild delete pods 'te*' -n default
kubectl wild get pods --prefix foo -n default   # equivalent to 'foo*'
kubectl wild get pods -p foo -n default        # short for --prefix
```

- Flags after the pattern are passed through to `kubectl` (e.g., `-n`, `-A`, `-l`).
- For `get`, output is rendered as a single kubectl table even across namespaces.
- For `describe`, the plugin runs `kubectl describe` on the matched set.
- For `delete`, the plugin shows matches and always asks for confirmation (`y/N`).

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


