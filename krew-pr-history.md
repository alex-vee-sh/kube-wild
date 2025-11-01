# Krew PR History and Templates

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
