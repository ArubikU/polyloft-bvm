import time
capacity = 100000
arr = list(range(capacity))
started = time.time() * 1000
s = 0
for i in range(capacity):
    s += arr[i]
ended = time.time() * 1000
print(s)
print("TIME_PY")
print(int(ended - started))
