#!/usr/bin/env python3
"""Cross-platform interleaved benchmark runner (CPython vs gopher-lua vs polyloft-bvm).

Self-times each program via its own `elapsed_ms=` line (startup/compile excluded),
interleaves the engines inside every iteration so machine drift cancels, verifies
that the functional output (everything except timing lines) is identical across
engines, and writes the RAW per-run samples plus environment metadata to JSON.
Statistics (median, CV, bootstrap 95% CI of the ratio of medians) are computed
from the raw samples, never from hand-entered numbers.

  python scripts/bench_runner.py --bvm PATH --lua PATH --n 30 --out results.json
"""
import argparse, json, os, platform, random, re, statistics, subprocess, sys, time

BENCHES = [
    ("fib", "bench_fib"), ("float", "bench_float"), ("string", "bench_string"),
    ("sort", "bench_sort"), ("array", "bench_array"), ("poly", "bench_poly"),
    ("closure", "bench_closure"), ("hash", "bench_hash"), ("alloc", "alloc_bench"),
    ("macro", "bench_macro"), ("macro_large", "bench_macro_large"),
    ("concurrent", "bench_concurrent"), ("io", "bench_io"),
]
ELAPSED = re.compile(r"elapsed_ms=([0-9.]+)")
TIMING = re.compile(r"(_ms|elapsed|time)", re.I)


def cpu_name():
    try:
        if sys.platform.startswith("linux"):
            for line in open("/proc/cpuinfo"):
                if line.startswith(("model name", "Model", "Hardware")):
                    return line.split(":", 1)[1].strip()
            out = subprocess.run(["lscpu"], capture_output=True, text=True).stdout
            for line in out.splitlines():
                if line.startswith("Model name"):
                    return line.split(":", 1)[1].strip()
        if sys.platform == "darwin":
            return subprocess.run(["sysctl", "-n", "machdep.cpu.brand_string"],
                                  capture_output=True, text=True).stdout.strip()
        if sys.platform == "win32":
            out = subprocess.run(["powershell", "-NoProfile", "-Command",
                                  "(Get-CimInstance Win32_Processor).Name"],
                                 capture_output=True, text=True).stdout
            return out.strip().splitlines()[0]
    except Exception:
        pass
    return platform.processor() or "unknown"


def go_version():
    try:
        return subprocess.run(["go", "version"], capture_output=True, text=True).stdout.split()[2].lstrip("go")
    except Exception:
        return ""


def run(cmd, timeout=600):
    p = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
    out = p.stdout
    m = ELAPSED.findall(out)
    if p.returncode != 0 or not m:
        return None, None
    functional = [l for l in out.splitlines()
                  if not TIMING.search(l.split("=")[0]) and "benchmark" not in l]
    return float(m[-1]), functional


def median(a): return statistics.median(a)
def cv(a):
    m = statistics.fmean(a)
    return 100 * statistics.pstdev(a) / m if m else 0.0


def boot_ratio_ci(num, den, iters=4000, seed=1):
    rng = random.Random(seed)
    rs = []
    for _ in range(iters):
        a = median([rng.choice(num) for _ in num])
        b = median([rng.choice(den) for _ in den])
        if b > 0: rs.append(a / b)
    rs.sort()
    return rs[int(0.025 * len(rs))], rs[int(0.975 * len(rs)) - 1]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--bvm", required=True)
    ap.add_argument("--lua")
    ap.add_argument("--base", help="interpreter built from the campaign's base commit")
    ap.add_argument("--base-only", default="fib,float,string,sort,array,poly",
                    help="benchmarks to run against --base (features of the base may be older)")
    ap.add_argument("--python", default=sys.executable)
    ap.add_argument("--programs", default="testdata/programs")
    ap.add_argument("--n", type=int, default=30)
    ap.add_argument("--warmup", type=int, default=2)
    ap.add_argument("--only", default="")
    ap.add_argument("--out", default="results.json")
    a = ap.parse_args()

    pyv = subprocess.run([a.python, "--version"], capture_output=True, text=True)
    env = {
        "host": platform.node(), "os": platform.platform(), "machine": platform.machine(),
        "cpu": cpu_name(), "cores": os.cpu_count(),
        "python": (pyv.stdout or pyv.stderr).strip().split()[-1],
        "date": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "go": go_version(),
        "n": a.n, "runner": os.environ.get("RUNNER_NAME", ""),
        "github_run": os.environ.get("GITHUB_RUN_ID", ""),
        "commit": os.environ.get("GITHUB_SHA", ""),
    }
    only = set(filter(None, a.only.split(",")))
    results, mismatches, notes = {}, [], []
    for name, stem in BENCHES:
        if only and name not in only: continue
        pf = os.path.join(a.programs, stem + ".pf")
        py = os.path.join(a.programs, stem + ".py")
        lu = os.path.join(a.programs, stem + ".lua")
        engines = {"bvm": [a.bvm, "run", pf]}
        if os.path.exists(py): engines["py"] = [a.python, py]
        if a.lua and os.path.exists(lu): engines["lua"] = [a.lua, lu]
        if a.base and name in set(a.base_only.split(",")): engines["base"] = [a.base, "run", pf]
        for _ in range(a.warmup):
            for cmd in engines.values(): run(cmd)
        samples = {k: [] for k in engines}
        func = {}
        for i in range(a.n):
            order = list(engines)
            random.Random(i).shuffle(order)          # randomize order within each iteration
            for k in order:
                t, f = run(engines[k])
                if t is not None:
                    samples[k].append(t); func.setdefault(k, f)
        # equal-work check: every functional key=value present in both engines must agree
        # (case-insensitive: True/true); keys an engine does not print are reported, not hidden
        def kv(lines):
            return {l.split("=", 1)[0]: l.split("=", 1)[1].strip().lower()
                    for l in (lines or []) if "=" in l}
        ref = kv(func.get("py") or func.get("bvm"))
        for k, f in func.items():
            if k == "py": continue
            other = kv(f)
            bad = [key for key in other if key in ref and other[key] != ref[key]]
            if bad: mismatches.append(name + ":" + k + ":" + ",".join(bad))
            missing = [key for key in ref if key not in other]
            if missing: notes.append(name + ":" + k + " omits " + ",".join(missing))
        entry = {"samples": samples}
        for k, s in samples.items():
            if s:
                entry[k] = {"median": median(s), "cv": cv(s), "min": min(s), "n": len(s)}
        if samples.get("base") and samples.get("py"):
            lo, hi = boot_ratio_ci(samples["base"], samples["py"])
            entry["ratio_start"] = {"ratio": median(samples["base"]) / median(samples["py"]),
                                    "ci_lo": lo, "ci_hi": hi}
        for base in ("py", "lua"):
            if samples.get("bvm") and samples.get(base):
                lo, hi = boot_ratio_ci(samples["bvm"], samples[base])
                entry["ratio_" + base] = {
                    "ratio": median(samples["bvm"]) / median(samples[base]),
                    "ci_lo": lo, "ci_hi": hi}
        results[name] = entry
        r = entry.get("ratio_py", {})
        print(f"{name:12s} bvm={entry.get('bvm',{}).get('median',0):9.1f}ms "
              f"cv={entry.get('bvm',{}).get('cv',0):4.1f}% "
              f"py={entry.get('py',{}).get('median',0):9.1f}ms "
              f"ratio_py={r.get('ratio',0):.2f} [{r.get('ci_lo',0):.2f},{r.get('ci_hi',0):.2f}]",
              flush=True)
    json.dump({"env": env, "mismatches": mismatches, "notes": notes, "results": results},
              open(a.out, "w"), indent=1)
    if mismatches: print("WARNING output mismatch:", mismatches, file=sys.stderr)


if __name__ == "__main__":
    main()
