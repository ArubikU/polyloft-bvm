import time
started = time.perf_counter()
N = 40000
parts = []
for i in range(N):
    parts.append("user" + str(i % 1000) + ",action" + str(i % 50) + ",ok\n")
content = "".join(parts)
path = "iobench_tmp.txt"
with open(path, "w") as f:
    f.write(content)
total_read = 0
for r in range(20):
    with open(path) as f:
        data = f.read()
    total_read += len(data)
ended = time.perf_counter()
print("python io benchmark")
print("content_len=" + str(len(content)))
print("total_read=" + str(total_read))
print("elapsed_ms=" + str((ended - started) * 1000.0))
