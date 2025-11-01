#!/usr/bin/env bash
set -euo pipefail

echo "[1] Status filter (Running, -A)"
../kubectl-wild get po -A --pod-status Running | head -n 20 || true

echo "[2] Fuzzy against hashed pod name (api-1 ~ apu-1)"
../kubectl-wild get po -n dev-x --fuzzy --fuzzy-distance 1 --match apu-1 || true

echo "[3] Contains via --match"
../kubectl-wild get po -n dev-x --contains --match pi- || true

echo "[4] Namespace filter (prod- prefix)"
../kubectl-wild get po -A --ns-prefix prod- || true

echo "[5] Age filters: younger-than 10m"
../kubectl-wild get po -A --younger-than 10m | head -n 10 || true

echo "[6] Age filters: older-than 1m (may need to wait a minute)"
../kubectl-wild get po -A --older-than 1m | head -n 10 || true

echo "[7] Delete preview (dry-run)"
../kubectl-wild delete cm -n dev-x -p te --dry-run || true


