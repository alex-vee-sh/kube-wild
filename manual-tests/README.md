# kube-wild manual test suite

This folder contains manifests and helper scripts to manually validate kube-wild features without relying on an existing cluster state.

Contents:
- 00-namespaces.yaml: Creates test namespaces (dev-x, prod-a, prod-b, ns-alexv-test)
- 10-deploy-running.yaml: Deployments producing Running pods across namespaces
- 11-pod-crashloop.yaml: A pod that CrashLoopBackOff
- 12-pod-pending.yaml: A pod that stays Pending (unschedulable)
- 13-job-completed.yaml: A Job that completes (Completed pod)
- 20-services.yaml: Services for resource matching
- 30-configmaps.yaml: ConfigMaps for delete/preview testing
- kustomization.yaml: Aggregates all above
- apply.sh / cleanup.sh: Convenience scripts

Usage:
- Apply all resources:
  ./apply.sh
- Remove all resources:
  ./cleanup.sh

Feature coverage (examples):
- Wildcards/prefix/contains:
  kubectl wild get po -n dev-x --match 'api-*'
  kubectl wild get po -n dev-x --contains web
  kubectl wild get po -n dev-x -p api
- Regex:
  kubectl wild get po -n dev-x --regex '^(api|web)-'
- Fuzzy:
  kubectl wild get po -n dev-x --fuzzy --fuzzy-distance 1 --match apu-1
- Namespace filters:
  kubectl wild get po -A --ns-prefix prod-
  kubectl wild get po -A --ns dev-x
- Status filters:
  kubectl wild get po -A --pod-status Running
  kubectl wild get po -A --pod-status CrashLoopBackOff
  kubectl wild get po -A --pod-status Pending
  kubectl wild get po -A --pod-status Completed
- Age filters (apply, then run soon/after wait):
  kubectl wild get po -A --younger-than 10m
  kubectl wild get po -A --older-than 1h
- Delete safety/preview:
  kubectl wild delete cm -n dev-x 'te*' --preview table
  kubectl wild delete cm -n dev-x 'te*' -y --dry-run
- All-namespaces table:
  kubectl wild get po -A --match '*'

Tips:
- For contains mode, provide the pattern via --match (e.g., --contains --match pi-).
- For fuzzy mode against hashed pod names, match a stable prefix (e.g., apu-1 vs api-1-<hash>).
