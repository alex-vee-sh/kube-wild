#!/usr/bin/env bash
set -euo pipefail
kubectl delete -k . --ignore-not-found=true
