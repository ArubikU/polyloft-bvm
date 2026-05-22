import time
capacity = 100000
arr = list(range(capacity))
started = time.time() * 1000
s = 0
for i in range(capacity):
    idx = (i * 137) % capacity
    s += arr[idx]
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
