# CPython counterpart of bench_concurrent.pf. Same four-thread structure over
# the same disjoint integer ranges. On this CPU-bound work CPython's threads are
# serialized by the GIL, so the wall time does not fall with more threads; the
# comparison against the interpreter (which runs each worker on a Go goroutine
# with an isolated VM) is therefore an honest architectural one. Integer-only, so
# the per-worker output is bit-identical to the .pf version.
import time
from concurrent.futures import ThreadPoolExecutor


def work(lo, hi):
    s = 0
    for i in range(lo, hi):
        s += i % 997
    return s


started = time.perf_counter()
with ThreadPoolExecutor(max_workers=4) as ex:
    fs = [ex.submit(work, lo, lo + 4000000) for lo in (0, 4000000, 8000000, 12000000)]
    rs = [f.result() for f in fs]
print("p1=" + str(rs[0]))
print("p2=" + str(rs[1]))
print("p3=" + str(rs[2]))
print("p4=" + str(rs[3]))
ended = time.perf_counter()

print("python concurrent benchmark")
print("workers=4")
print("elapsed_ms=" + str((ended - started) * 1000.0))
