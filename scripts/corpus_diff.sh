#!/usr/bin/env bash
# Differential check: run every program in tests/ with two binaries and compare the
# functional output (timing lines removed). Usage: corpus_diff.sh NEW REF
NEW="$1"; REF="$2"; fail=0; n=0
for f in tests/*.pf tests/**/*.pf testdata/programs/*.pf testdata/regress/*.pf; do
  [ -f "$f" ] || continue
  n=$((n+1))
  a="$(timeout 120 "$NEW" run "$f" 2>&1 | grep -vE '_ms=|elapsed|time|^[0-9]+.[0-9]{4,}$' )"
  b="$(timeout 120 "$REF" run "$f" 2>&1 | grep -vE '_ms=|elapsed|time|^[0-9]+.[0-9]{4,}$' )"
  if [ "$a" != "$b" ]; then echo "DIFF: $f"; fail=$((fail+1)); fi
done
echo "programs: $n, differing: $fail"
