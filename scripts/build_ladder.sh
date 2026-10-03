#!/usr/bin/env bash
# Build one interpreter binary per commit of the optimization ladder (for the in-CI
# attribution study). Older commits used a local giocss replace; we reuse HEAD's go.mod/go.sum.
set -euo pipefail
OUT="${1:-out/ladder}"; EXT=""; [ "${RUNNER_OS:-}" = "Windows" ] && EXT=".exe"
mkdir -p "$OUT"; OUT="$(cd "$OUT" && pwd)"
# name=commit   (order = chronological)
LADDER="L0_start=dbd86d8 L1_stackptr=232628f L2_fusion=0b55a42 L3_preinline=c024a36 L4_inline=97ed07f L5_memo_fast=59d0f24 L6_dense=b4946fc"
for kv in $LADDER; do
  name="${kv%%=*}"; sha="${kv##*=}"
  dir="$(mktemp -d)"
  git worktree add -q --detach "$dir" "$sha"
  cp go.mod go.sum "$dir/"
  ( cd "$dir" && go build -o "$OUT/$name$EXT" ./cmd/polyloft-bvm ) && echo "built $name ($sha)" || echo "FAILED $name ($sha)" >&2
  git worktree remove --force "$dir"
done
ls -la "$OUT"
