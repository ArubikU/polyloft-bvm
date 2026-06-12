#!/usr/bin/env bash
# Wall-clock interleaved benchmark harness for the polyloft-bvm CPython-parity
# study. Runs each .pf benchmark against its line-for-line CPython counterpart,
# interleaving the two so slow machine drift cancels, and reports median, CV,
# and min for both sides plus the interpreter/CPython ratio. Emits a plain
# table and a LaTeX tabular ready to paste into the paper.
#
# Designed to run on Linux / WSL2 / macOS / git-bash. Self-times via each
# program's own `elapsed_ms` output (not the process wall time), so startup and
# compilation overhead are excluded.
#
# Usage:
#   scripts/wallclock_bench.sh [N]          # N runs per side (default 30)
#   N=50 scripts/wallclock_bench.sh
#
# Output goes to stdout; redirect to capture, e.g.
#   scripts/wallclock_bench.sh 30 | tee bench_$(hostname)_$(uname -s).txt

# Resilient on purpose: a transient failure in one run must not abort the whole
# sweep, and grep returning "no match" must not kill the pipeline.
set -u
cd "$(dirname "$0")/.."

N="${1:-${N:-30}}"
PROG_DIR="testdata/programs"
BIN="$(mktemp -d)/polyloft-bvm"

# --- locate toolchain ----------------------------------------------------------
GO_BIN="$(command -v go || true)"
[ -z "$GO_BIN" ] && { echo "go not found in PATH" >&2; exit 1; }
PY_BIN="$(command -v python3 || command -v python || true)"
[ -z "$PY_BIN" ] && { echo "python3/python not found in PATH" >&2; exit 1; }

echo "Building interpreter ..." >&2
if ! "$GO_BIN" build -o "$BIN" ./cmd/polyloft-bvm; then
  echo "build failed" >&2; exit 1
fi
# Verify the chosen Python actually runs (guards against the Windows Store stub).
if ! "$PY_BIN" --version >/dev/null 2>&1; then
  echo "python at '$PY_BIN' is not runnable (Windows Store stub?). Use WSL or a real python3." >&2
  exit 1
fi

# --- environment banner --------------------------------------------------------
CPU="$(grep -m1 'model name' /proc/cpuinfo 2>/dev/null | sed 's/.*: //' || \
       sysctl -n machdep.cpu.brand_string 2>/dev/null || echo 'unknown CPU')"
echo "# polyloft-bvm wall-clock benchmark"
echo "# host    : $(hostname)"
echo "# os      : $(uname -srm)"
echo "# cpu     : $CPU"
echo "# go      : $("$GO_BIN" version | awk '{print $3}')"
echo "# python  : $("$PY_BIN" --version 2>&1 | awk '{print $2}')"
echo "# n       : $N (interleaved, self-timed elapsed_ms)"
echo "# date    : $(date -u '+%Y-%m-%dT%H:%M:%SZ')"
echo

# benchmarks: <name> <pf-stem>  (pf and py share the stem)
BENCHES="fib bench_fib
float bench_float
string bench_string
sort bench_sort
array bench_array
poly bench_poly
closure bench_closure
hash bench_hash
alloc alloc_bench
macro bench_macro"

# elapsed_ms extractor
elapsed() { "$@" 2>/dev/null | grep -oE 'elapsed_ms=[0-9.]+' | tail -1 | cut -d= -f2; }

# stats: median mean cv% min  (from stdin, one number per line)
stats() {
  sort -n | awk '
    { a[NR]=$1; s+=$1; ss+=$1*$1 }
    END {
      n=NR; if(n==0){print "0 0 0 0"; exit}
      m=s/n
      med=(n%2)?a[(n+1)/2]:(a[n/2]+a[n/2+1])/2
      sd=sqrt((ss/n)-(m*m)); if(sd<0)sd=0
      printf "%.3f %.3f %.1f %.3f", med, m, (m?100*sd/m:0), a[1]
    }'
}

printf "%-8s | %10s %5s %6s | %10s %5s %6s | %7s\n" \
  bench "py_med" "CV%" "py_min" "interp" "CV%" "i_min" "ratio"
echo "---------+--------------------------+--------------------------+--------"

LATEX="$(mktemp)"
{
  echo "% --- paste into the paper (median / CV / ratio) ---"
  echo "\\begin{tabular}{@{}lrrrrr@{}}"
  echo "  \\toprule"
  echo "  Benchmark & CPython & CV & Interp.\\ & CV & Ratio \\\\"
  echo "  \\midrule"
} > "$LATEX"

while read -r name stem; do
  [ -z "$name" ] && continue
  pf="$PROG_DIR/$stem.pf"; py="$PROG_DIR/$stem.py"
  [ -f "$pf" ] && [ -f "$py" ] || { printf "%-8s | (missing program)\n" "$name"; continue; }
  py_runs=""; in_runs=""
  for _ in $(seq 1 "$N"); do
    py_runs+="$(elapsed "$PY_BIN" "$py")"$'\n'
    in_runs+="$(elapsed "$BIN" run "$pf")"$'\n'
  done
  read -r pmed pmean pcv pmin <<<"$(printf '%s' "$py_runs" | stats)"
  read -r imed imean icv imin <<<"$(printf '%s' "$in_runs" | stats)"
  ratio="$(awk -v a="$imed" -v b="$pmed" 'BEGIN{printf "%.2f", (b>0)?a/b:0}')"
  printf "%-8s | %10s %5s %6s | %10s %5s %6s | %6sx\n" \
    "$name" "$pmed" "$pcv" "$pmin" "$imed" "$icv" "$imin" "$ratio"
  bold=""; [ "$(awk -v r="$ratio" 'BEGIN{print (r<1)?1:0}')" = "1" ] && bold="\\mathbf"
  awk -v n="$name" -v pm="$pmed" -v pc="$pcv" -v im="$imed" -v ic="$icv" -v r="$ratio" -v b="$bold" \
    'BEGIN{printf "  %s & $%s$\\,ms & $%s\\%%$ & $%s$\\,ms & $%s\\%%$ & $%s{%sx}$ \\\\\n", n, pm, pc, im, ic, (b==""?"":b), r}' >> "$LATEX"
done <<< "$BENCHES"

{ echo "  \\bottomrule"; echo "\\end{tabular}"; } >> "$LATEX"
echo
cat "$LATEX"
rm -f "$LATEX"
rm -rf "$(dirname "$BIN")"
