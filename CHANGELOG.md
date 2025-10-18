# Changelog

## v0.0.3
- Namespace filters: `--ns`, `--ns-prefix`, `--ns-regex`
- Safety: `--confirm-threshold`, `--server-dry-run`
- Previews: default table on -A; red list shows NAMESPACE\tRESOURCE/NAME
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
