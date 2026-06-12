#!/usr/bin/env bash
# Wall-clock interleaved benchmark harness for the polyloft-bvm CPython-parity
# study. Each benchmark is run against up to two baselines: its line-for-line
# CPython counterpart (.py) and, when present, an equal-work gopher-lua version
# (.lua) executed by a pure-Go Lua interpreter. The Lua baseline is the key
# control: it is itself an interpreter written in Go, so comparing against it
# isolates the cost of *this* interpreter's engineering from the cost of the Go
# host language. The three engines are interleaved so machine drift cancels;
# median, coefficient of variation, and min are reported for each, plus the
# interpreter/CPython and interpreter/gopher-lua ratios. A LaTeX tabular is
# emitted ready to paste.
#
# Self-times via each program's own elapsed_ms output, so startup/compile cost is
# excluded. Runs on Linux / WSL2 / macOS / git-bash.
#
# Usage:  scripts/wallclock_bench.sh [N]      (N runs per engine, default 30)
#   scripts/wallclock_bench.sh 30 | tee bench_$(hostname).txt

set -u
cd "$(dirname "$0")/.."

N="${1:-${N:-30}}"
PROG_DIR="testdata/programs"
TMP="$(mktemp -d)"
BIN="$TMP/polyloft-bvm"
LUABIN="$TMP/luabench"

GO_BIN="$(command -v go || true)"
[ -z "$GO_BIN" ] && { echo "go not found in PATH" >&2; exit 1; }
PY_BIN="$(command -v python3 || command -v python || true)"

echo "Building interpreter ..." >&2
if ! "$GO_BIN" build -o "$BIN" ./cmd/polyloft-bvm; then echo "build failed" >&2; exit 1; fi

# gopher-lua baseline: separate module, only built if it compiles (needs the
# gopher-lua dependency; first build fetches it).
HAVE_LUA=0
echo "Building gopher-lua baseline ..." >&2
if ( cd bench/luabench && "$GO_BIN" build -o "$LUABIN" . ) 2>/dev/null; then HAVE_LUA=1
else echo "gopher-lua baseline unavailable (skipping Lua column)" >&2; fi

# CPython guard (the Windows Store stub is not runnable).
HAVE_PY=0
if [ -n "$PY_BIN" ] && "$PY_BIN" --version >/dev/null 2>&1; then HAVE_PY=1
else echo "python3 not runnable (skipping CPython column); use WSL/native Linux" >&2; fi

CPU="$(grep -m1 'model name' /proc/cpuinfo 2>/dev/null | sed 's/.*: //' || \
       sysctl -n machdep.cpu.brand_string 2>/dev/null || echo 'unknown CPU')"
echo "# polyloft-bvm wall-clock benchmark"
echo "# host    : $(hostname)"
echo "# os      : $(uname -srm)"
echo "# cpu     : $CPU"
echo "# go      : $("$GO_BIN" version | awk '{print $3}')"
[ "$HAVE_PY" = 1 ] && echo "# python  : $("$PY_BIN" --version 2>&1 | awk '{print $2}')"
[ "$HAVE_LUA" = 1 ] && echo "# lua     : gopher-lua (pure-Go interpreter)"
echo "# n       : $N (interleaved, self-timed elapsed_ms)"
echo "# date    : $(date -u '+%Y-%m-%dT%H:%M:%SZ')"
echo

BENCHES="fib bench_fib
float bench_float
string bench_string
sort bench_sort
array bench_array
poly bench_poly
closure bench_closure
hash bench_hash
alloc alloc_bench
macro bench_macro
macro_large bench_macro_large
concurrent bench_concurrent"

elapsed() { "$@" 2>/dev/null | grep -oE 'elapsed_ms=[0-9.]+' | tail -1 | cut -d= -f2; }
stat() {  # median mean cv% min
  sort -n | awk '
    { a[NR]=$1; s+=$1; ss+=$1*$1 }
    END { n=NR; if(n==0){print "- - - -"; exit}
      m=s/n; med=(n%2)?a[(n+1)/2]:(a[n/2]+a[n/2+1])/2
      sd=sqrt((ss/n)-(m*m)); if(sd<0)sd=0
      printf "%.2f %.2f %.1f %.2f", med, m, (m?100*sd/m:0), a[1] }'
}
rat() { awk -v a="$1" -v b="$2" 'BEGIN{ if(b+0>0) printf "%.2f", a/b; else printf "-" }'; }

printf "%-10s | %9s %5s | %9s %5s | %9s %5s | %6s %6s\n" \
  bench "py_med" "CV%" "lua_med" "CV%" "polyloft" "CV%" "pl/py" "pl/lua"
echo "-----------+-----------------+-----------------+-----------------+--------------"

LX="$TMP/table.tex"
{ echo "% --- paste into the paper ---"
  echo "\\begin{tabular}{@{}lrrrrr@{}}"
  echo "  \\toprule"
  echo "  Benchmark & CPython & gopher-lua & Polyloft & Polyloft/Py & Polyloft/Lua \\\\"
  echo "  \\midrule"; } > "$LX"

while read -r name stem; do
  [ -z "$name" ] && continue
  pf="$PROG_DIR/$stem.pf"; py="$PROG_DIR/$stem.py"; luaf="$PROG_DIR/$stem.lua"
  [ -f "$pf" ] || continue
  py_runs=""; lua_runs=""; in_runs=""
  for _ in $(seq 1 "$N"); do
    [ "$HAVE_PY" = 1 ] && [ -f "$py" ] && py_runs+="$(elapsed "$PY_BIN" "$py")"$'\n'
    [ "$HAVE_LUA" = 1 ] && [ -f "$luaf" ] && lua_runs+="$(elapsed "$LUABIN" "$luaf")"$'\n'
    in_runs+="$(elapsed "$BIN" run "$pf")"$'\n'
  done
  read -r pmed pmean pcv pmin <<<"$(printf '%s' "$py_runs"  | stat)"
  read -r lmed lmean lcv lmin <<<"$(printf '%s' "$lua_runs" | stat)"
  read -r imed imean icv imin <<<"$(printf '%s' "$in_runs"  | stat)"
  ipy="$(rat "$imed" "$pmed")"; ilua="$(rat "$imed" "$lmed")"
  printf "%-10s | %9s %5s | %9s %5s | %9s %5s | %6s %6s\n" \
    "$name" "$pmed" "$pcv" "$lmed" "$lcv" "$imed" "$icv" "$ipy" "$ilua"
  awk -v n="$name" -v p="$pmed" -v l="$lmed" -v im="$imed" -v ipy="$ipy" -v ilua="$ilua" \
    'BEGIN{ pf=(p=="-"?"--":"$"p"$\\,ms"); lf=(l=="-"?"--":"$"l"$\\,ms");
      printf "  %s & %s & %s & $%s$\\,ms & $%s$ & $%s$ \\\\\n", n, pf, lf, im, ipy, ilua }' >> "$LX"
done <<< "$BENCHES"

{ echo "  \\bottomrule"; echo "\\end{tabular}"; } >> "$LX"
echo; cat "$LX"
rm -rf "$TMP"
