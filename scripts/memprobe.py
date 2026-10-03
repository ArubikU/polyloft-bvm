#!/usr/bin/env python3
"""Peak memory + time of one binary on some benchmarks, optionally under several GOGC values.

  python scripts/memprobe.py BIN [--gogc 100,50,25] [--bench bench_poly,...] [--n 3]
"""
import argparse, os, re, statistics as st, subprocess, sys
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import bench_runner as r

ap = argparse.ArgumentParser()
ap.add_argument("bin")
ap.add_argument("--gogc", default="")
ap.add_argument("--bench", default="bench_macro_large,bench_poly,bench_array,bench_sort,bench_macro,bench_closure")
ap.add_argument("--n", type=int, default=3)
ap.add_argument("--programs", default="testdata/programs")
a = ap.parse_args()

for g in (a.gogc.split(",") if a.gogc else [""]):
    env = dict(os.environ)
    if g:
        env["GOGC"] = g
    print(f"GOGC={g or 'default'}")
    for b in a.bench.split(","):
        ts, ms = [], []
        for _ in range(a.n):
            p = subprocess.Popen([a.bin, "run", os.path.join(a.programs, b + ".pf")],
                                 stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, env=env, text=True)
            out, _ = p.communicate()
            kb = r._win_peak_kb(p._handle) if sys.platform == "win32" else None
            if kb:
                ms.append(kb / 1024)
            m = re.findall(r"elapsed_ms=([0-9.]+)", out)
            if m:
                ts.append(float(m[-1]))
        print(f"  {b:18s} {st.median(ms) if ms else 0:7.1f} MB  {st.median(ts) if ts else 0:7.0f} ms")
